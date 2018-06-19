package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/loomnetwork/loomchain/e2e/lib"
	"github.com/loomnetwork/loomchain/e2e/node"
)

type engineCmd struct {
	conf  lib.Config
	tests lib.Tests
	wg    *sync.WaitGroup
	errC  chan error
}

func NewCmd(conf lib.Config, tc lib.Tests) Engine {
	return &engineCmd{
		conf:  conf,
		tests: tc,
		wg:    &sync.WaitGroup{},
		errC:  make(chan error),
	}
}

func (e *engineCmd) Run(ctx context.Context, eventC chan *node.Event) error {
	for _, n := range e.tests.TestCases {
		// evaluate template
		t, err := template.New("cmd").Parse(n.RunCmd)
		if err != nil {
			return err
		}
		buf := new(bytes.Buffer)
		err = t.Execute(buf, e.conf)
		if err != nil {
			return err
		}

		fmt.Printf("--> run: %s\n", buf.String())
		args := strings.Split(buf.String(), " ")
		if len(args) == 0 {
			return errors.New("missing command")
		}
		iter := n.Iterations
		if iter == 0 {
			iter = 1
		}

		dir := e.conf.BaseDir
		if n.Dir != "" {
			dir = n.Dir
		}
		for i := 0; i < iter; i++ {
			cmd := exec.Cmd{
				Dir:  dir,
				Path: args[0],
				Args: args,
			}
			if n.Delay > 0 {
				time.Sleep(time.Duration(n.Delay) * time.Millisecond)
			}

			out, err := cmd.Output()
			if err != nil {
				fmt.Printf("--> error: %s\n", err)
				continue
			}
			fmt.Printf("--> output:\n%s\n", out)

			var expecteds []string
			for _, expected := range n.Expected {
				t, err = template.New("expected").Parse(expected)
				if err != nil {
					return err
				}
				buf = new(bytes.Buffer)
				err = t.Execute(buf, e.conf)
				if err != nil {
					return err
				}
				expecteds = append(expecteds, buf.String())
			}

			switch n.Condition {
			case "contains":
				for _, expected := range expecteds {
					if !strings.Contains(string(out), expected) {
						return fmt.Errorf("❌ expect output to contain '%s'", expected)
					}
				}
			}
		}
	}

	return nil
}
