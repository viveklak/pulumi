// Copyright 2016-2018, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package display

// forked from: https://github.com/moby/moby/blob/master/pkg/jsonmessage/jsonmessage.go
// so we can customize parts of the display of our progress messages

import (
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	gotty "github.com/ijc/Gotty"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/pulumi/pulumi/pkg/v3/engine"
	"github.com/pulumi/pulumi/sdk/v3/go/common/diag/colors"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/cmdutil"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
)

/* Satisfied by gotty.TermInfo as well as noTermInfo from below */
type termInfo interface {
	Parse(attr string, params ...interface{}) (string, error)
}

type noTermInfo struct{} // canary used when no terminfo.

func (ti *noTermInfo) Parse(attr string, params ...interface{}) (string, error) {
	return "", fmt.Errorf("noTermInfo")
}

func clearLine(out io.Writer, ti termInfo) {
	// el2 (clear whole line) is not exposed by terminfo.

	// First clear line from beginning to cursor
	if attr, err := ti.Parse("el1"); err == nil {
		fmt.Fprintf(out, "%s", attr)
	} else {
		fmt.Fprintf(out, "\x1b[1K")
	}
	// Then clear line from cursor to end
	if attr, err := ti.Parse("el"); err == nil {
		fmt.Fprintf(out, "%s", attr)
	} else {
		fmt.Fprintf(out, "\x1b[K")
	}
}

func cursorUp(out io.Writer, ti termInfo, l int) {
	if l == 0 { // Should never be the case, but be tolerant
		return
	}
	if attr, err := ti.Parse("cuu", l); err == nil {
		fmt.Fprintf(out, "%s", attr)
	} else {
		fmt.Fprintf(out, "\x1b[%dA", l)
	}
}

func cursorDown(out io.Writer, ti termInfo, l int) {
	if l == 0 { // Should never be the case, but be tolerant
		return
	}
	if attr, err := ti.Parse("cud", l); err == nil {
		fmt.Fprintf(out, "%s", attr)
	} else {
		fmt.Fprintf(out, "\x1b[%dB", l)
	}
}

// Progress describes a message we want to show in the display.  There are two types of messages,
// simple 'Messages' which just get printed out as a single uninterpreted line, and 'Actions' which
// are placed and updated in the progress-grid based on their ID.  Messages do not need an ID, while
// Actions must have an ID.
type Progress struct {
	ID      string
	Message string
	Action  string
}

func makeMessageProgress(message string) Progress {
	return Progress{Message: message}
}

func makeActionProgress(id string, action string) Progress {
	contract.Assertf(id != "", "id must be non empty for action %s", action)
	contract.Assertf(action != "", "action must be non empty")

	return Progress{ID: id, Action: action}
}

// Display displays the Progress to `out`. `termInfo` is non-nil if `out` is a terminal.
func (jm *Progress) Display(out io.Writer, termInfo termInfo) {
	var endl string
	if termInfo != nil && /*jm.Stream == "" &&*/ jm.Action != "" {
		clearLine(out, termInfo)
		endl = "\r"
		fmt.Fprint(out, endl)
	}

	if jm.Action != "" && termInfo != nil {
		fmt.Fprintf(out, "%s%s", jm.Action, endl)
	} else {
		var msg string
		if jm.Action != "" {
			msg = jm.Action
		} else {
			msg = jm.Message
		}

		fmt.Fprintf(out, "%s%s\n", msg, endl)
	}
}

type messageRenderer struct {
	opts Options

	isTerminal bool

	// The width and height of the terminal.  Used so we can trim resource messages that are too long.
	terminalWidth  int
	terminalHeight int

	// A spinner to use to show that we're still doing work even when no output has been
	// printed to the console in a while.
	nonInteractiveSpinner cmdutil.Spinner

	progressOutput chan<- Progress

	// Cache of lines we've already printed.  We don't print a progress message again if it hasn't
	// changed between the last time we printed and now.
	printedProgressCache map[string]Progress
}

func (r *messageRenderer) Close() error {
	close(r.progressOutput)
	return nil
}

// Converts the colorization tags in a progress message and then actually writes the progress
// message to the output stream.  This should be the only place in this file where we actually
// process colorization tags.
func (r *messageRenderer) colorizeAndWriteProgress(progress Progress) {
	if progress.Message != "" {
		progress.Message = r.opts.Color.Colorize(progress.Message)
	}

	if progress.Action != "" {
		progress.Action = r.opts.Color.Colorize(progress.Action)
	}

	if progress.ID != "" {
		// don't repeat the same output if there is no difference between the last time we
		// printed it and now.
		lastProgress, has := r.printedProgressCache[progress.ID]
		if has && lastProgress.Message == progress.Message && lastProgress.Action == progress.Action {
			return
		}

		r.printedProgressCache[progress.ID] = progress
	}

	if !r.isTerminal {
		// We're about to display something.  Reset our spinner so that it will go on the next line.
		r.nonInteractiveSpinner.Reset()
	}

	r.progressOutput <- progress
}

func (r *messageRenderer) writeSimpleMessage(msg string) {
	r.colorizeAndWriteProgress(makeMessageProgress(msg))
}

func (r *messageRenderer) writeBlankLine() {
	r.writeSimpleMessage(" ")
}

func (r *messageRenderer) println(display *ProgressDisplay, line string) {
	r.writeSimpleMessage(line)
}

func (r *messageRenderer) tick(display *ProgressDisplay) {
	if r.isTerminal {
		r.render(display)
	} else {
		// Update the spinner to let the user know that that work is still happening.
		r.nonInteractiveSpinner.Tick()
	}
}

func (r *messageRenderer) renderRow(display *ProgressDisplay,
	id string, colorizedColumns []string, maxColumnLengths []int) {

	uncolorizedColumns := display.uncolorizeColumns(colorizedColumns)

	row := renderRow(colorizedColumns, uncolorizedColumns, maxColumnLengths)
	if r.isTerminal {
		// Ensure we don't go past the end of the terminal.  Note: this is made complex due to
		// msgWithColors having the color code information embedded with it.  So we need to get
		// the right substring of it, assuming that embedded colors are just markup and do not
		// actually contribute to the length
		maxRowLength := r.terminalWidth - 1
		if maxRowLength < 0 {
			maxRowLength = 0
		}
		row = colors.TrimColorizedString(row, maxRowLength)
	}

	if row != "" {
		if r.isTerminal {
			r.colorizeAndWriteProgress(makeActionProgress(id, row))
		} else {
			r.writeSimpleMessage(row)
		}
	}
}

func (r *messageRenderer) rowUpdated(display *ProgressDisplay, row Row) {
	if r.isTerminal {
		// if we're in a terminal, then refresh everything so that all our columns line up
		r.render(display)
	} else {
		// otherwise, just print out this single row.
		colorizedColumns := row.ColorizedColumns()
		colorizedColumns[display.suffixColumn] += row.ColorizedSuffix()
		r.renderRow(display, "", colorizedColumns, nil)
	}
}

func (r *messageRenderer) systemMessage(display *ProgressDisplay, payload engine.StdoutEventPayload) {
	if r.isTerminal {
		// if we're in a terminal, then refresh everything.  The system events will come after
		// all the normal rows
		r.render(display)
	} else {
		// otherwise, in a non-terminal, just print out the actual event.
		r.writeSimpleMessage(renderStdoutColorEvent(payload, display.opts))
	}
}

func (r *messageRenderer) done(display *ProgressDisplay) {
}

func (r *messageRenderer) render(display *ProgressDisplay) {
	if !r.isTerminal || display.headerRow == nil {
		return
	}

	// make sure our stored dimension info is up to date
	r.updateTerminalDimensions()

	rootNodes := display.generateTreeNodes()
	rootNodes = display.filterOutUnnecessaryNodesAndSetDisplayTimes(rootNodes)
	sortNodes(rootNodes)
	display.addIndentations(rootNodes, true /*isRoot*/, "")

	maxSuffixLength := 0
	for _, v := range display.suffixesArray {
		runeCount := utf8.RuneCountInString(v)
		if runeCount > maxSuffixLength {
			maxSuffixLength = runeCount
		}
	}

	var rows [][]string
	var maxColumnLengths []int
	display.convertNodesToRows(rootNodes, maxSuffixLength, &rows, &maxColumnLengths)

	removeInfoColumnIfUnneeded(rows)

	for i, row := range rows {
		r.renderRow(display, fmt.Sprintf("%v", i), row, maxColumnLengths)
	}

	systemID := len(rows)

	printedHeader := false
	for _, payload := range display.systemEventPayloads {
		msg := payload.Color.Colorize(payload.Message)
		lines := splitIntoDisplayableLines(msg)

		if len(lines) == 0 {
			continue
		}

		if !printedHeader {
			printedHeader = true
			r.colorizeAndWriteProgress(makeActionProgress(
				fmt.Sprintf("%v", systemID), " "))
			systemID++

			r.colorizeAndWriteProgress(makeActionProgress(
				fmt.Sprintf("%v", systemID),
				colors.Yellow+"System Messages"+colors.Reset))
			systemID++
		}

		for _, line := range lines {
			r.colorizeAndWriteProgress(makeActionProgress(
				fmt.Sprintf("%v", systemID), fmt.Sprintf("  %s", line)))
			systemID++
		}
	}
}

// Ensure our stored dimension info is up to date.
func (r *messageRenderer) updateTerminalDimensions() {
	currentTerminalWidth, currentTerminalHeight, err := terminal.GetSize(int(os.Stdout.Fd()))
	contract.IgnoreError(err)

	if currentTerminalWidth != r.terminalWidth ||
		currentTerminalHeight != r.terminalHeight {
		r.terminalWidth = currentTerminalWidth
		r.terminalHeight = currentTerminalHeight

		// also clear our display cache as we want to reprint all lines.
		r.printedProgressCache = make(map[string]Progress)
	}
}

// ShowProgressOutput displays a progress stream from `in` to `out`, `isTerminal` describes if
// `out` is a terminal. If this is the case, it will print `\n` at the end of each line and move the
// cursor while displaying.
func ShowProgressOutput(in <-chan Progress, out io.Writer, isTerminal bool) {
	var (
		ids = make(map[string]int)
	)

	var info termInfo

	if isTerminal {
		term := os.Getenv("TERM")
		if term == "" {
			term = "vt102"
		}

		var err error
		if info, err = gotty.OpenTermInfo(term); err != nil {
			info = &noTermInfo{}
		}
	}

	for jm := range in {
		diff := 0

		if jm.Action != "" {
			if jm.ID == "" {
				contract.Failf("Must have an ID if we have an action! %s", jm.Action)
			}

			line, ok := ids[jm.ID]
			if !ok {
				// NOTE: This approach of using len(id) to
				// figure out the number of lines of history
				// only works as long as we clear the history
				// when we output something that's not
				// accounted for in the map, such as a line
				// with no ID.
				line = len(ids)
				ids[jm.ID] = line
				if info != nil {
					fmt.Fprintf(out, "\n")
				}
			}
			diff = len(ids) - line
			if info != nil {
				cursorUp(out, info, diff)
			}
		} else {
			// When outputting something that isn't progress
			// output, clear the history of previous lines. We
			// don't want progress entries from some previous
			// operation to be updated (for example, pull -a
			// with multiple tags).
			ids = make(map[string]int)
		}
		jm.Display(out, info)
		if jm.Action != "" && info != nil {
			cursorDown(out, info, diff)
		}
	}
}
