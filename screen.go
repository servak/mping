package mping

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/nsf/termbox-go"
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
	const coldef = termbox.ColorDefault
	termbox.Clear(coldef, coldef)
	length := 0
	for k, v := range totalStats {
		if l := len(v.hostname) + len(k); l > length {
			length = l
		}
	}

	hostFormat := "%-" + strconv.Itoa(length+2) + "s "
	header := fmt.Sprintf(hostFormat+"%-7s %-5s %-12s %-12s %-12s %-12s",
		"Host", "Loss%", "Sent", "Last", "Avg", "Best", "Worst")
	bFormat := hostFormat + "%5.1f%% %5d %12v %12v %12v %12v"
	tbPrint(0, 0, termbox.ColorMagenta, coldef, "Press 'q' to quit")

	bold := coldef | termbox.AttrBold
	tbPrint(0, 2, bold, coldef, header)

	_results := make(map[int]string)
	for k, v := range totalStats {
		host := v.hostname
		if host != k {
			host = fmt.Sprintf("%s(%s)", v.hostname, k)
		}

		line := fmt.Sprintf(bFormat, host, v.loss(), v.count, v.last, v.average(), v.min, v.max)
		_results[v.order] = line
	}

	var keys []int
	for k := range _results {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	baseline := 3
	for _, k := range keys {
		tbPrint(0, baseline, coldef, coldef, _results[k])
		baseline++
	}

	termbox.Flush()
}

func printScreenValues() {
	length := 0
	for k, v := range totalStats {
		if l := len(v.hostname) + len(k); l > length {
			length = l
		}
	}

	hostFormat := "%-" + strconv.Itoa(length+2) + "s "
	bFormat := hostFormat + "%5.1f%% %5d %12v %12v %12v %12v"
	fmt.Printf(hostFormat+"%-7s %-5s %-12s %-12s %-12s %-12s\n",
		"Host", "Loss%", "Sent", "Last", "Avg", "Best", "Worst")

	_results := make(map[int]string)
	for k, v := range totalStats {
		host := v.hostname
		if host != k {
			host = fmt.Sprintf("%s(%s)", v.hostname, k)
		}

		line := fmt.Sprintf(bFormat, host, v.loss(), v.count, v.last, v.average(), v.min, v.max)
		_results[v.order] = line
	}

	var keys []int
	for k := range _results {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	for _, k := range keys {
		fmt.Println(_results[k])
	}
}

func updateView() {
	t := time.NewTicker(time.Millisecond * 250)
	for {
		select {
		case <-t.C:
			screenRedraw()
		}
	}
}
