package main

import "testing"

func TestShouldCreateDefaultAdmin(t *testing.T) {
	if shouldCreateDefaultAdmin(0, config{WebInstall: true}) {
		t.Fatal("default web install should not create admin on empty database")
	}
	if !shouldCreateDefaultAdmin(0, config{WebInstall: false}) {
		t.Fatal("disabled web install should create default admin")
	}
	if !shouldCreateDefaultAdmin(0, config{WebInstall: true, AdminExplicit: true}) {
		t.Fatal("explicit admin env should create default admin")
	}
	if shouldCreateDefaultAdmin(1, config{WebInstall: false, AdminExplicit: true}) {
		t.Fatal("existing users should not create default admin")
	}
}
