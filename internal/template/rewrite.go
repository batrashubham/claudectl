package template

import (
	"bufio"
	"io"
	"strings"
)

func RewriteSessionID(reader io.Reader, writer io.Writer, oldID, newID string) (int, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
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
