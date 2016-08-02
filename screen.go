package mping

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/nsf/termbox-go"
)

var sortType = 1
var currentPage = 0
var pageLength = 1
var reverse = false
var title = ""

const (
	coldef     = termbox.ColorDefault
	IgnoreLine = 3
)

func tbPrint(x, y int, fg, bg termbox.Attribute, msg string) {
	for _, c := range msg {
		termbox.SetCell(x, y, c, fg, bg)
		x++
	}
}

func screenInit() {
	err := termbox.Init()
	if err != nil {
		panic(err)
	}
	termbox.SetInputMode(termbox.InputAlt)
}

func screenClose() {
	termbox.Close()
}

func screenRedraw() {
	termbox.Clear(coldef, coldef)
	w, h := termbox.Size()
	pageSize := h - IgnoreLine
	pageLength = len(totalStats) / (pageSize + 1)
	// if change screen size
	if pageLength < currentPage {
		currentPage = 0
	}

	drawTop(currentPage, pageLength, w)

	header, body := drawTotalStats()
	bold := coldef | termbox.AttrBold
	tbPrint(0, 1, bold, coldef, header)
	begin := currentPage * pageSize
	end := (currentPage + 1) * pageSize

	var sliceBody []string
	if end > len(body) {
		sliceBody = body[begin:]
	} else {
		sliceBody = body[begin:end]
	}

	for i, v := range sliceBody {
		tbPrint(0, 2+i, coldef, coldef, v)
	}

	tbPrint(0, h-1, coldef, coldef, "q: quite program, n: next page, p: previous page, s: sort, r: reverse mode, R: count reset")
	termbox.Flush()
}

func drawTop(page, pageSize, width int) {
	keys := totalStats.keys()
	if sortType >= len(keys) {
		sortType = 0
	}
	msg := fmt.Sprintf("Sort: %s", keys[sortType])
	if reverse {
		msg += ", Reverse mode"
	}

	lmsg := fmt.Sprintf("[%d/%d]", page+1, pageSize+1)
	tbPrint(0, 0, termbox.ColorMagenta, coldef, msg)
	tbPrint(width/2, 0, termbox.ColorMagenta|termbox.AttrBold, coldef, title)
	tbPrint(width-len(lmsg), 0, termbox.ColorMagenta, coldef, lmsg)
}

func getSortInterface(s statistics, str string) (t sort.Interface) {
	switch str {
	case Host:
		t = byHost{s}
	case Success:
		t = bySuccess{s}
	case Loss, Fail:
		t = byLoss{s}
	case Best:
		t = byBest{s}
	case Last:
		t = byLast{s}
	case Avg:
		t = byAvg{s}
	case Worst:
		t = byWorst{s}
	}

	return
}

func drawTotalStats() (string, []string) {
	headers := totalStats.keys()
	length := make([]int, len(headers))
	for i, k := range headers {
		length[i] = totalStats.getMaxLength(k)
	}

	// print header
	msg := []string{}
	for i, h := range headers {
		msg = append(msg, fmt.Sprintf("%-"+strconv.Itoa(length[i])+"s", h))
	}
	header := strings.Join(msg, "  ")
	body := []string{}

	t := getSortInterface(totalStats, headers[sortType])
	if reverse {
		sort.Sort(sort.Reverse(t))
	} else {
		sort.Sort(t)
	}

	// print body
	for _, _stats := range totalStats {
		v := _stats.values()
		msg = []string{}
		for i, h := range headers {
			msg = append(msg, fmt.Sprintf("%"+strconv.Itoa(length[i])+"s", v[h]))
		}
		body = append(body, strings.Join(msg, "  "))
	}

	return header, body
}

func printScreenValues() {
	header, body := drawTotalStats()
	fmt.Println(header)
	for _, v := range body {
		fmt.Println(v)
	}

}
