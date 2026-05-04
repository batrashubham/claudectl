package template

import (
	"bytes"
	"strings"
	"testing"
)

func TestRewriteSessionID_AllOccurrencesReplaced(t *testing.T) {
	oldID := "abc12345-1234-5678-9abc-def012345678"
	newID := "new12345-1234-5678-9abc-def012345678"

	input := strings.Join([]string{
		`{"sessionId":"` + oldID + `","data":"hello"}`,
		`{"sessionId":"` + oldID + `","data":"world"}`,
		`{"ref":"` + oldID + `","nested":"` + oldID + `"}`,
	}, "\n")

	reader := strings.NewReader(input)
	var buf bytes.Buffer

	lineCount, err := RewriteSessionID(reader, &buf, oldID, newID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lineCount != 3 {
		t.Errorf("expected 3 lines, got %d", lineCount)
	}

	output := buf.String()
	if strings.Contains(output, oldID) {
		t.Errorf("output still contains old ID %q", oldID)
	}

	// Each original line had oldID; count occurrences of newID
	count := strings.Count(output, newID)
	// Line 1: 1 occurrence, Line 2: 1 occurrence, Line 3: 2 occurrences = 4 total
	if count != 4 {
		t.Errorf("expected 4 occurrences of new ID, got %d", count)
	}
}

func TestRewriteSessionID_UnrelatedUUIDsUnchanged(t *testing.T) {
	oldID := "abc12345-1234-5678-9abc-def012345678"
	newID := "new12345-1234-5678-9abc-def012345678"
	otherUUID := "ffffffff-ffff-ffff-ffff-ffffffffffff"

	input := `{"sessionId":"` + oldID + `","other":"` + otherUUID + `"}`

	reader := strings.NewReader(input)
	var buf bytes.Buffer

	_, err := RewriteSessionID(reader, &buf, oldID, newID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, otherUUID) {
		t.Error("unrelated UUID was incorrectly modified")
	}
	if strings.Contains(output, oldID) {
		t.Error("old ID was not replaced")
	}
	if !strings.Contains(output, newID) {
		t.Error("new ID not found in output")
	}
}

func TestRewriteSessionID_EmptyInput(t *testing.T) {
	reader := strings.NewReader("")
	var buf bytes.Buffer

	lineCount, err := RewriteSessionID(reader, &buf, "old", "new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lineCount != 0 {
		t.Errorf("expected 0 lines for empty input, got %d", lineCount)
	}

	if buf.String() != "" {
		t.Errorf("expected empty output, got %q", buf.String())
	}
}

func TestRewriteSessionID_LinesWithoutIDPassThrough(t *testing.T) {
	oldID := "abc12345-1234-5678-9abc-def012345678"
	newID := "new12345-1234-5678-9abc-def012345678"

	input := strings.Join([]string{
		`{"type":"system","data":"no session id here"}`,
		`{"sessionId":"` + oldID + `","data":"has id"}`,
		`plain text line with no JSON`,
	}, "\n")

	reader := strings.NewReader(input)
	var buf bytes.Buffer

	lineCount, err := RewriteSessionID(reader, &buf, oldID, newID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lineCount != 3 {
		t.Errorf("expected 3 lines, got %d", lineCount)
	}

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")

	if lines[0] != `{"type":"system","data":"no session id here"}` {
		t.Errorf("line without ID was modified: %q", lines[0])
	}
	if lines[2] != `plain text line with no JSON` {
		t.Errorf("plain text line was modified: %q", lines[2])
	}
	if !strings.Contains(lines[1], newID) {
		t.Errorf("line with ID was not rewritten: %q", lines[1])
	}
}
