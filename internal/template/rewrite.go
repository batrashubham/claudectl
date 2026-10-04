package template

import (
	"bufio"
	"io"
	"strings"
)

func RewriteSessionID(reader io.Reader, writer io.Writer, oldID, newID string) (int, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
	bw := bufio.NewWriter(writer)

	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		rewritten := strings.ReplaceAll(line, oldID, newID)
		if _, err := bw.WriteString(rewritten + "\n"); err != nil {
			return lineCount, err
		}
		lineCount++
	}

	if err := bw.Flush(); err != nil {
		return lineCount, err
	}
	return lineCount, scanner.Err()
}

// Transcript lines can embed whole files or base64 images, well past
// bufio's defaults; a template save must not fail on them.
const maxLineBytes = 64 * 1024 * 1024
