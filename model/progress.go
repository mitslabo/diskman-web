package model

import (
	"regexp"
	"strconv"
)

var (
	rePass     = regexp.MustCompile(`Pass\s+(\d+)`)
	rePct      = regexp.MustCompile(`pct rescued:\s+([\d.]+)%`)
	reRescued  = regexp.MustCompile(`rescued:\s+([^,]+)`)
	reRate     = regexp.MustCompile(`current rate:\s+([^,]+)`)
	reRemain   = regexp.MustCompile(`remaining time:\s+(.+?)\s*$`)
	reBadAreas = regexp.MustCompile(`bad areas:\s+(\d+)`)
	reReadErr  = regexp.MustCompile(`read errors:\s+(\d+)`)
)

func ParseProgressLine(line string, prev Progress) Progress {
	p := prev
	if m := rePass.FindStringSubmatch(line); len(m) == 2 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			p.Pass = v
		}
	}
	if m := rePct.FindStringSubmatch(line); len(m) == 2 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			p.Percent = v
		}
	}
	if m := reRescued.FindStringSubmatch(line); len(m) == 2 {
		p.Rescued = m[1]
	}
	if m := reRate.FindStringSubmatch(line); len(m) == 2 {
		p.Rate = m[1]
	}
	if m := reRemain.FindStringSubmatch(line); len(m) == 2 {
		p.Remaining = m[1]
	}
	if m := reBadAreas.FindStringSubmatch(line); len(m) == 2 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			p.BadAreas = v
		}
	}
	if m := reReadErr.FindStringSubmatch(line); len(m) == 2 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			p.ReadErrs = v
		}
	}
	return p
}
