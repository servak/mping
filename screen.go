package mping

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/nsf/termbox-go"
)

var (
	currentPage = 0
	pageLength  = 1
	reverse     = false
	title       = ""
	maxRtt      = 0
)

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
	err := termbox.Clear(coldef, coldef)
	if err != nil {
		return
	}
	w, h := termbox.Size()
	pageSize := h - IgnoreLine
	pageLength = len(statTable.values) / (pageSize + 1)
	// if change screen size
	if pageLength < currentPage {
		currentPage = 0
	}

	drawTop(currentPage, pageLength, w)

	header, body := drawstatTable()
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

	tbPrint(0, h-1, coldef, coldef, "q: quit program, n: next page, p: previous page, s: sort, r: reverse mode, R: count reset")
	termbox.Flush()
}

func drawTop(page, pageSize, width int) {
	msg := fmt.Sprintf("Sort: %s", statTable.sortType)
	if reverse {
		msg += ", Reverse mode"
	}
	msg += fmt.Sprintf(", Interval: %dms", maxRtt)

	lmsg := fmt.Sprintf("[%d/%d]", page+1, pageSize+1)
	tbPrint(0, 0, termbox.ColorMagenta, coldef, msg)
	tbPrint(width/2, 0, termbox.ColorMagenta|termbox.AttrBold, coldef, title)
	tbPrint(width-len(lmsg), 0, termbox.ColorMagenta, coldef, lmsg)
}

func drawstatTable() (string, []string) {
	headers := statTable.keys()
	length := make([]int, len(headers))
	for i, k := range headers {
		length[i] = statTable.getMaxLength(k)
	}

	// print header
	msg := []string{}
	for i, h := range headers {
		msg = append(msg, fmt.Sprintf("%-"+strconv.Itoa(length[i])+"s", h))
	}
	header := strings.Join(msg, "  ")
	body := []string{}

	if reverse {
		sort.Sort(sort.Reverse(statTable))
	} else {
		sort.Sort(statTable)
	}

	// print body
	for _, _stats := range statTable.values {
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
	header, body := drawstatTable()
	fmt.Println(header)
	for _, v := range body {
		fmt.Println(v)
	}

}
