package main

import "testing"

func TestAdminRejectsArguments(t *testing.T) {
	if code := run([]string{"--password=bad"}); code == 0 {
		t.Fatal("password argument was accepted")
	}
	if code := run([]string{"reset"}); code == 0 {
		t.Fatal("reset argument was accepted")
	}
}

func TestAdminAllowsReadOnlyVersion(t *testing.T) {
	if code := run([]string{"version"}); code != 0 {
		t.Fatalf("version exit code = %d", code)
	}
}

func TestAdminAllowsReadOnlyDoctor(t *testing.T) {
	if code := run([]string{"doctor"}); code != 0 {
		t.Fatalf("doctor exit code = %d", code)
	}
}
