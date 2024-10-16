package streampanel

import (
	"context"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type errorReport struct {
	LastTimestamp time.Time
	Error         error
	Stack         []byte
}

func (p *Panel) ReportError(err error) {
	logger.Debugf(p.defaultContext, "ReportError('%v')", err)
	defer logger.Debugf(p.defaultContext, "/ReportError('%v')", err)
	if err == nil {
		return
	}

	p.statusPanelSet(fmt.Sprintf("Error: %v", err))

	ctx := context.TODO()
	p.errorReportsLocker.Do(ctx, func() {
		p.errorReports[err.Error()] = errorReport{
			LastTimestamp: time.Now(),
			Error:         err,
			Stack:         debug.Stack(),
		}
	})
}

func (p *Panel) getErrorReports() []errorReport {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &p.errorReportsLocker, func() []errorReport {
		result := make([]errorReport, 0, len(p.errorReports))
		for _, report := range p.errorReports {
			result = append(result, report)
		}

		sort.Slice(result, func(i, j int) bool {
			return result[i].LastTimestamp.After(result[j].LastTimestamp)
		})

		return result
	})
}

func (p *Panel) statusPanelSet(text string) {
	ctx := context.TODO()
	p.statusPanelLocker.Do(ctx, func() {
		if p.statusPanel == nil {
			return
		}
		p.statusPanel.SetText("status:  " + text)
	})
}

func (p *Panel) ShowErrorReports() {
	logger.Debugf(p.defaultContext, "ShowErrorReports()")
	defer logger.Debugf(p.defaultContext, "/ShowErrorReports()")

	reports := p.getErrorReports()

	content := container.NewVBox()
	if len(reports) == 0 {
		content.Add(
			widget.NewRichTextWithText(
				"No significant errors were reported since the application was started, yet...",
			),
		)
	} else {
		for _, report := range reports {
			errLabel := report.Error.Error()
			if len(errLabel) > 60 {
				errLabel = "..." + errLabel[len(errLabel)-60:]
			}
			content.Add(widget.NewButton(fmt.Sprintf("%s -- %v", report.LastTimestamp.Format("2006-01-02 15:04:05"), errLabel), func() {
				w := p.app.NewWindow(fmt.Sprintf("Error report '%v'", report.Error))
				resizeWindow(w, fyne.NewSize(600, 600))
				t := widget.NewRichTextWithText(fmt.Sprintf("%s -- %v\n\nstack:\n%s", report.LastTimestamp.Format("2006-01-02 15:04:05"), report.Error, report.Stack))
				t.Wrapping = fyne.TextWrapWord
				w.SetContent(container.NewVBox(
					t,
				))
				w.Show()
			}))
		}
	}

	w := p.app.NewWindow("Error reports")
	resizeWindow(w, fyne.NewSize(600, 600))
	w.SetContent(content)
	w.Show()
}

func (p *Panel) DisplayError(err error) {
	logger.Debugf(p.defaultContext, "DisplayError('%v')", err)
	defer logger.Debugf(p.defaultContext, "/DisplayError('%v')", err)

	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "context canceled") {
		return
	}

	p.ReportError(err)

	errorMessage := fmt.Sprintf("Error: %v\n\nstack trace:\n%s", err, debug.Stack())
	textWidget := widget.NewMultiLineEntry()
	textWidget.SetText(errorMessage)
	textWidget.Wrapping = fyne.TextWrapWord
	textWidget.TextStyle = fyne.TextStyle{
		Bold:      true,
		Monospace: true,
	}

	ctx := context.TODO()
	p.displayErrorLocker.Do(ctx, func() {
		if p.lastDisplayedError != nil {
			// protection against flood:
			if err.Error() == p.lastDisplayedError.Error() {
				return
			}
		}
		p.lastDisplayedError = err

		if p.displayErrorWindow != nil {
			p.displayErrorWindow.SetContent(textWidget)
			return
		}
		w := p.app.NewWindow(AppName + ": Got an error: " + err.Error())
		resizeWindow(w, fyne.NewSize(400, 300))
		w.SetContent(textWidget)

		w.SetOnClosed(func() {
			p.displayErrorLocker.Do(ctx, func() {
				p.displayErrorWindow = nil
			})
		})
		w.Show()
		logger.Tracef(p.defaultContext, "DisplayError(): w.Show()")
		p.displayErrorWindow = w
	})
}
