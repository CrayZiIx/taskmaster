package tui

// History stores submitted lines and tracks navigation state for browsing
// them with the up/down arrow keys, supervisorctl/readline-style: browsing
// never mutates the stored history, and editing a recalled line only
// changes the live buffer.
type History struct {
	items []string
	idx   int    // -1 = not currently browsing (live buffer is authoritative)
	saved string // live-buffer snapshot taken when browsing starts
}

func NewHistory() *History {
	return &History{idx: -1}
}

// Add appends a submitted line, skipping empty lines and immediate
// duplicates of the last entry.
func (h *History) Add(line string) {
	if line == "" {
		return
	}
	if len(h.items) > 0 && h.items[len(h.items)-1] == line {
		return
	}
	h.items = append(h.items, line)
}

// Prev moves one step back in history (toward older entries), returning the
// line to display. current is the live buffer's contents, snapshotted the
// first time Prev is called so Next can restore it later.
func (h *History) Prev(current string) (string, bool) {
	if len(h.items) == 0 {
		return "", false
	}
	if h.idx == -1 {
		h.saved = current
		h.idx = len(h.items) - 1
	} else if h.idx > 0 {
		h.idx--
	}
	return h.items[h.idx], true
}

// Next moves one step forward in history (toward newer entries). Once past
// the newest entry it restores the live-buffer snapshot and stops browsing.
func (h *History) Next() (string, bool) {
	if h.idx == -1 {
		return "", false
	}
	h.idx++
	if h.idx >= len(h.items) {
		h.idx = -1
		return h.saved, true
	}
	return h.items[h.idx], true
}

// ResetBrowse ends any in-progress history browsing. Called after a line is
// submitted and after Ctrl-C cancels the current line.
func (h *History) ResetBrowse() {
	h.idx = -1
}
