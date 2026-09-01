package tui

// History stores submitted lines and tracks navigation state for browsing
// them with the up/down arrow keys.
type History struct {
	items []string
	idx   int
	saved string
}

func NewHistory() *History { return &History{idx: -1} }

func (h *History) Add(line string) {
	if line == "" || (len(h.items) > 0 && h.items[len(h.items)-1] == line) {
		return
	}
	h.items = append(h.items, line)
}

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

func (h *History) ResetBrowse() { h.idx = -1 }
