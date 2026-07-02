package main

import "testing"

func TestPersistentPreRunERejectsJSONOnUnsupportedCommands(t *testing.T) {
	saved := flagJSON
	t.Cleanup(func() { flagJSON = saved })

	flagJSON = true
	if err := rootCmd.PersistentPreRunE(installCmd, nil); err == nil {
		t.Error("expected --json to be rejected on `paq install`, got nil error")
	}
	if err := rootCmd.PersistentPreRunE(doctorCmd, nil); err == nil {
		t.Error("expected --json to be rejected on `paq doctor`, got nil error")
	}
}

func TestPersistentPreRunEAllowsJSONOnSupportedCommands(t *testing.T) {
	saved := flagJSON
	t.Cleanup(func() { flagJSON = saved })

	flagJSON = true
	if err := rootCmd.PersistentPreRunE(lsCmd, nil); err != nil {
		t.Errorf("expected --json to be allowed on `paq ls`, got: %v", err)
	}
	if err := rootCmd.PersistentPreRunE(registryShowCmd, nil); err != nil {
		t.Errorf("expected --json to be allowed on `paq registry show`, got: %v", err)
	}
}

func TestPersistentPreRunEAllowsNonJSONInvocations(t *testing.T) {
	saved := flagJSON
	t.Cleanup(func() { flagJSON = saved })

	flagJSON = false
	if err := rootCmd.PersistentPreRunE(installCmd, nil); err != nil {
		t.Errorf("expected no error without --json, got: %v", err)
	}
}
