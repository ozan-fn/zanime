// WebVTT parsing and cue helpers.
package subtitle

import "strings"

// ParseVTT splits a WebVTT file into the header block and the cue blocks,
// keeping raw lines so timing and cue ids survive a round trip.
func ParseVTT(vtt string) (header string, blocks [][]string) {
	for _, raw := range strings.Split(strings.ReplaceAll(vtt, "\r\n", "\n"), "\n\n") {
		block := strings.Split(strings.TrimRight(raw, "\n"), "\n")
		if len(block) == 1 && block[0] == "" {
			continue
		}
		if header == "" && strings.HasPrefix(block[0], "WEBVTT") {
			header = strings.Join(block, "\n")
			continue
		}
		blocks = append(blocks, block)
	}
	return header, blocks
}

func RebuildVTT(header string, blocks [][]string) string {
	var b strings.Builder
	if header != "" {
		b.WriteString(header)
		b.WriteString("\n\n")
	}
	for _, blk := range blocks {
		b.WriteString(strings.Join(blk, "\n"))
		b.WriteString("\n\n")
	}
	return b.String()
}

// TimingIndex locates the "-->" line of a cue block, which is not always at a
// fixed offset: a block may carry a cue id line before it.
func TimingIndex(block []string) int {
	for i, line := range block {
		if strings.Contains(line, "-->") {
			return i
		}
	}
	return -1
}

// CueText joins everything after the timing line of a cue block.
func CueText(block []string) string {
	i := TimingIndex(block)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(block[i+1:], " "))
}

func SetCueText(block []string, text string) []string {
	i := TimingIndex(block)
	if i < 0 {
		return block
	}
	out := make([]string, 0, i+2)
	out = append(out, block[:i+1]...)
	return append(out, text)
}
