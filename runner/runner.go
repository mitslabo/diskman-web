//go:build !windows

package runner

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"diskman-web/model"
)

type Update struct {
	JobID     string
	Progress  model.Progress
	State     model.JobState
	Err       error
	Completed bool
	Cancelled bool
}

var (
	reDdBytes = regexp.MustCompile(`^(\d+) bytes`)
	reDdRate  = regexp.MustCompile(`,\s+([\d.]+ \S+/s)`)
)

// StartJob launches a job goroutine and sends updates to out.
func StartJob(ctx context.Context, job model.Job, out chan<- Update) {
	go func() {
		out <- Update{JobID: job.ID, State: model.JobRunning}

		if job.Op == "erase" {
			err := runErase(ctx, job, out)
			if err != nil {
				if ctx.Err() != nil {
					out <- Update{JobID: job.ID, State: model.JobCancelled, Cancelled: true}
					return
				}
				out <- Update{JobID: job.ID, State: model.JobError, Err: err}
				return
			}
			out <- Update{JobID: job.ID, State: model.JobDone, Completed: true}
			return
		}

		// copy: ddrescue 3-pass
		if err := os.MkdirAll(filepath.Dir(job.MapFile), 0o755); err != nil {
			out <- Update{JobID: job.ID, State: model.JobError, Err: err}
			return
		}
		for pass := 1; pass <= 3; pass++ {
			select {
			case <-ctx.Done():
				out <- Update{JobID: job.ID, State: model.JobCancelled, Cancelled: true}
				return
			default:
			}
			if err := runRealPass(ctx, job, pass, out); err != nil {
				if ctx.Err() != nil {
					out <- Update{JobID: job.ID, State: model.JobCancelled, Cancelled: true}
					return
				}
				out <- Update{JobID: job.ID, State: model.JobError, Err: err}
				return
			}
		}
		out <- Update{JobID: job.ID, State: model.JobDone, Completed: true}
	}()
}

func runRealPass(ctx context.Context, job model.Job, pass int, out chan<- Update) error {
	args := buildArgs(pass, job.Src, job.Dst, job.MapFile)
	cmd := exec.CommandContext(ctx, "ddrescue", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	p := model.Progress{Pass: pass}
	errCh := make(chan error, 2)
	var stderrBuf bytes.Buffer
	go func() { errCh <- readProgress(stdout, job.ID, p, out, nil) }()
	go func() { errCh <- readProgress(stderr, job.ID, p, out, &stderrBuf) }()

	waitErr := cmd.Wait()
	_ = <-errCh
	_ = <-errCh
	if waitErr != nil {
		if msg := strings.TrimSpace(stderrBuf.String()); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return waitErr
	}
	return nil
}

// scanLines splits on \n, \r\n, or bare \r.
func scanLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		if b == '\r' || b == '\n' {
			j := i + 1
			if j < len(data) && b == '\r' && data[j] == '\n' {
				j++
			}
			return j, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func readProgress(r io.Reader, jobID string, base model.Progress, out chan<- Update, capture *bytes.Buffer) error {
	s := bufio.NewScanner(r)
	s.Split(scanLines)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	p := base
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		if capture != nil {
			capture.WriteString(line + "\n")
		}
		p = model.ParseProgressLine(line, p)
		out <- Update{JobID: jobID, Progress: p, State: model.JobRunning}
	}
	return s.Err()
}

func buildArgs(pass int, src, dst, mapFile string) []string {
	switch pass {
	case 1:
		return []string{"--force", "-n", src, dst, mapFile}
	case 2:
		return []string{"--force", "-r3", src, dst, mapFile}
	default:
		return []string{"--force", "-R", src, dst, mapFile}
	}
}

// deviceSize returns the byte size of a block device or file.
func deviceSize(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return f.Seek(0, io.SeekEnd)
}

func runErase(ctx context.Context, job model.Job, out chan<- Update) error {
	totalBytes, _ := deviceSize(job.Dst)

	cmd := exec.CommandContext(ctx, "dd",
		"if=/dev/zero",
		"of="+job.Dst,
		"bs=1M",
	)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// status=progress が使えない古い dd のため SIGUSR1 を定期送信
	sigCtx, cancelSig := context.WithCancel(ctx)
	go func() {
		defer cancelSig()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-sigCtx.Done():
				return
			case <-ticker.C:
				if cmd.Process != nil {
					_ = cmd.Process.Signal(syscall.SIGUSR1)
				}
			}
		}
	}()

	p := model.Progress{Pass: 1}
	var stderrBuf bytes.Buffer
	errCh := make(chan error, 1)
	go func() {
		errCh <- readEraseProgress(stderr, job.ID, p, totalBytes, out, &stderrBuf)
	}()

	waitErr := cmd.Wait()
	cancelSig()
	_ = <-errCh

	if waitErr != nil {
		stderrText := stderrBuf.String()
		if strings.Contains(stderrText, "No space left on device") {
			return nil
		}
		for _, line := range strings.Split(stderrText, "\n") {
			if strings.HasPrefix(line, "dd: ") {
				return fmt.Errorf("%s", line)
			}
		}
		return waitErr
	}
	return nil
}

func readEraseProgress(r io.Reader, jobID string, base model.Progress, totalBytes int64, out chan<- Update, stderrBuf *bytes.Buffer) error {
	s := bufio.NewScanner(r)
	s.Split(scanLines)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	p := base
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		stderrBuf.WriteString(line + "\n")
		p = parseDdProgress(line, p, totalBytes)
		out <- Update{JobID: jobID, Progress: p, State: model.JobRunning}
	}
	return s.Err()
}

func parseDdProgress(line string, prev model.Progress, totalBytes int64) model.Progress {
	p := prev
	written := int64(-1)
	if m := reDdBytes.FindStringSubmatch(line); len(m) == 2 {
		if w, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			written = w
			p.Rescued = formatBytes(written)
			if totalBytes > 0 {
				p.Percent = float64(written) / float64(totalBytes) * 100
				if p.Percent > 100 {
					p.Percent = 100
				}
			}
		}
	}
	if m := reDdRate.FindStringSubmatch(line); len(m) == 2 {
		p.Rate = strings.TrimSpace(m[1])
	}
	if totalBytes > 0 && written >= 0 {
		if written >= totalBytes {
			p.Remaining = "0s"
		} else if eta := estimateRemaining(totalBytes, written, p.Rate); eta != "" {
			p.Remaining = eta
		}
	}
	return p
}

func estimateRemaining(totalBytes, writtenBytes int64, rateStr string) string {
	if totalBytes <= 0 || writtenBytes >= totalBytes {
		return "0s"
	}
	bps, ok := parseRateBytesPerSec(rateStr)
	if !ok || bps <= 0 {
		return ""
	}
	remainingBytes := totalBytes - writtenBytes
	seconds := int64(math.Ceil(float64(remainingBytes) / bps))
	if seconds < 0 {
		seconds = 0
	}
	return formatDuration(seconds)
}

func parseRateBytesPerSec(rateStr string) (float64, bool) {
	parts := strings.Fields(rateStr)
	if len(parts) != 2 {
		return 0, false
	}
	v, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, false
	}
	unit := strings.TrimSuffix(parts[1], "/s")
	mult, ok := map[string]float64{
		"B":   1,
		"kB":  1e3,
		"KB":  1e3,
		"MB":  1e6,
		"GB":  1e9,
		"TB":  1e12,
		"KiB": 1024,
		"MiB": 1024 * 1024,
		"GiB": 1024 * 1024 * 1024,
		"TiB": 1024 * 1024 * 1024 * 1024,
	}[unit]
	if !ok {
		return 0, false
	}
	return v * mult, true
}

func formatDuration(seconds int64) string {
	if seconds <= 0 {
		return "0s"
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

