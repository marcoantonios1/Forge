package patch

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type ParseError struct {
	Line    int
	Message string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("patch: parse error at line %d: %s", e.Line, e.Message)
}

// ParsePatchSet parses a unified diff string into a PatchSet.
// Accepts standard git diff output or simplified --- / +++ format.
func ParsePatchSet(sessionID, taskID, diffText string) (*PatchSet, error) {
	lines := strings.Split(diffText, "\n")
	ps := &PatchSet{
		SessionID: sessionID,
		TaskID:    taskID,
		CreatedAt: time.Now(),
	}

	var cur *Patch
	var curHunk *Hunk

	flushHunk := func() {
		if curHunk != nil && cur != nil {
			cur.Hunks = append(cur.Hunks, *curHunk)
			curHunk = nil
		}
	}
	flushPatch := func() {
		flushHunk()
		if cur != nil {
			ps.Patches = append(ps.Patches, *cur)
			cur = nil
		}
	}

	for i, line := range lines {
		lineNum := i + 1

		// Binary file marker — hard error.
		if strings.HasPrefix(line, "Binary files") && strings.HasSuffix(line, "differ") {
			return nil, &ParseError{Line: lineNum, Message: "binary file patches are not supported: " + line}
		}

		// New file header (git format).
		if strings.HasPrefix(line, "diff --git ") {
			flushPatch()
			cur = &Patch{}
			continue
		}

		// --- line: old file (ignore in git format; used for path in simple format).
		if strings.HasPrefix(line, "--- ") {
			if cur == nil {
				cur = &Patch{}
			}
			continue
		}

		// +++ line: extract path.
		if strings.HasPrefix(line, "+++ ") {
			flushHunk()
			if cur == nil {
				cur = &Patch{}
			}
			path := strings.TrimPrefix(line, "+++ ")
			path = strings.TrimPrefix(path, "b/")
			// Strip any tab-separated timestamp that some diff tools append.
			if idx := strings.IndexByte(path, '\t'); idx != -1 {
				path = path[:idx]
			}
			cur.Path = strings.TrimSpace(path)
			continue
		}

		// Hunk header.
		if strings.HasPrefix(line, "@@ ") {
			flushHunk()
			if cur == nil {
				return nil, &ParseError{Line: lineNum, Message: "hunk header before file header"}
			}
			h, err := parseHunkHeader(line, lineNum)
			if err != nil {
				return nil, err
			}
			curHunk = h
			continue
		}

		// Hunk body lines.
		if curHunk != nil {
			switch {
			case line == "" || line == "\\ No newline at end of file":
				// skip these meta lines
			case strings.HasPrefix(line, " "),
				strings.HasPrefix(line, "+"),
				strings.HasPrefix(line, "-"):
				curHunk.Lines = append(curHunk.Lines, line)
			}
			continue
		}
	}

	flushPatch()
	return ps, nil
}

func parseHunkHeader(line string, lineNum int) (*Hunk, error) {
	// Format: @@ -OldStart[,OldLines] +NewStart[,NewLines] @@[ context]
	end := strings.Index(line[3:], "@@")
	if end == -1 {
		return nil, &ParseError{Line: lineNum, Message: "malformed hunk header: " + line}
	}
	inner := strings.TrimSpace(line[3 : end+3])
	parts := strings.Fields(inner)
	if len(parts) < 2 {
		return nil, &ParseError{Line: lineNum, Message: "malformed hunk header: " + line}
	}

	oldStart, oldLines, err := parseRange(parts[0], lineNum)
	if err != nil {
		return nil, err
	}
	newStart, newLines, err := parseRange(parts[1], lineNum)
	if err != nil {
		return nil, err
	}

	return &Hunk{
		OldStart: oldStart,
		OldLines: oldLines,
		NewStart: newStart,
		NewLines: newLines,
	}, nil
}

// parseRange parses "-10,6" or "+10,6" or "-10" into (start, count).
func parseRange(s string, lineNum int) (start, count int, err error) {
	s = strings.TrimLeft(s, "+-")
	parts := strings.SplitN(s, ",", 2)
	start, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, &ParseError{Line: lineNum, Message: "invalid range: " + s}
	}
	if len(parts) == 2 {
		count, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, &ParseError{Line: lineNum, Message: "invalid range count: " + s}
		}
	} else {
		count = 1
	}
	return start, count, nil
}
