package plugin

import (
	"context"
	"reflect"
	"testing"
)

func TestHookPriorityIsStable(t *testing.T) {
	manager := NewManager()
	manager.Register(hookTestPlugin{name: "priority", init: func(m *Manager) {
		m.RegisterHookWithPriority("custom.pipeline", HookPriorityLate, appendHook("late-first"))
		m.RegisterHookWithPriority("custom.pipeline", HookPriorityEarly, appendHook("early"))
		m.RegisterHookWithPriority("custom.pipeline", HookPriorityLate, appendHook("late-second"))
		m.RegisterHook("custom.pipeline", appendHook("normal"))
	}})
	manager.SetActivePlugins([]string{"priority"})

	result, err := manager.ApplyActive(context.Background(), "custom.pipeline", []string{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"early", "normal", "late-first", "late-second"}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("hook order = %#v, want %#v", result, want)
	}
}

func TestHookDispatchSignalAndActiveFiltering(t *testing.T) {
	manager := NewManager()
	manager.Register(hookTestPlugin{name: "stopping", init: func(m *Manager) {
		m.RegisterHook("custom.takeover", func(_ context.Context, payload any) (any, error) {
			values := append(payload.([]string), "stopped")
			return StopHook(values), nil
		})
		m.RegisterHook("custom.takeover", appendHook("must-not-run"))
	}})

	dispatch, err := manager.DispatchActive(context.Background(), "custom.takeover", []string{})
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.Triggered || dispatch.Stopped {
		t.Fatalf("inactive dispatch = %#v", dispatch)
	}
	manager.SetActivePlugins([]string{"stopping"})
	dispatch, err = manager.DispatchActive(context.Background(), "custom.takeover", []string{})
	if err != nil {
		t.Fatal(err)
	}
	if !dispatch.Triggered || !dispatch.Stopped {
		t.Fatalf("active dispatch = %#v", dispatch)
	}
	if want := []string{"stopped"}; !reflect.DeepEqual(dispatch.Payload, want) {
		t.Fatalf("dispatch payload = %#v, want %#v", dispatch.Payload, want)
	}

	missing, err := manager.DispatchActive(context.Background(), "custom.missing", "original")
	if err != nil {
		t.Fatal(err)
	}
	if missing.Triggered || missing.Stopped || missing.Payload != "original" {
		t.Fatalf("missing dispatch = %#v", missing)
	}
}

func appendHook(value string) HookFunc {
	return func(_ context.Context, payload any) (any, error) {
		return append(payload.([]string), value), nil
	}
}

type hookTestPlugin struct {
	name string
	init func(*Manager)
}

func (p hookTestPlugin) Name() string        { return p.name }
func (p hookTestPlugin) Version() string     { return "1.0.0" }
func (p hookTestPlugin) Description() string { return "hook test plugin" }
func (p hookTestPlugin) Init(manager *Manager) {
	p.init(manager)
}
