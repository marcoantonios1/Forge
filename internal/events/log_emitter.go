package events

import (
	"encoding/json"
	"fmt"
	"os"
)

type LogEmitter struct {
	Debug bool
}

func (e *LogEmitter) Emit(ev Event) {
	tool, _ := ev.Payload["tool"].(string)

	switch ev.Type {
	case EventToolInvoked:
		args, _ := ev.Payload["args"].(map[string]any)
		argsJSON, _ := json.Marshal(args)
		fmt.Fprintf(os.Stderr, "[event] %-14s %-16s %s\n", ev.Type, tool, argsJSON)

	case EventToolOutput:
		ok, _ := ev.Payload["ok"].(bool)
		summary, _ := ev.Payload["summary"].(string)
		if e.Debug {
			fmt.Fprintf(os.Stderr, "[event] %-14s %-16s ok=%-5v %q\n", ev.Type, tool, ok, summary)
		}
	}
}
