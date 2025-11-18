package spinner

import (
	"context"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
)

type Spinner struct {
	s *spinner.Spinner
}

func New(message string) *Spinner {
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Suffix = " " + message
	s.FinalMSG = ""

	return &Spinner{
		s: s,
	}
}

func (sp *Spinner) Start(ctx context.Context) {
	sp.s.Start()

	go func() {
		<-ctx.Done()
		if sp.s.Active() {
			sp.s.Stop()
		}
	}()
}

func (sp *Spinner) Stop(message string) {
	green := color.New(color.FgGreen).SprintFunc()
	sp.s.FinalMSG = green("✔") + " " + message + "\n"
	sp.s.Stop()
}

func (sp *Spinner) Fail(message string) {
	red := color.New(color.FgRed).SprintFunc()
	sp.s.FinalMSG = red("✖") + " " + message + "\n"
	sp.s.Stop()
}

func (sp *Spinner) UpdateMessage(message string) {
	sp.s.Suffix = " " + message
}

func (sp *Spinner) IsRunning() bool {
	return sp.s.Active()
}
