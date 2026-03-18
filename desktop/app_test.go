package main

import "testing"

func TestBuildLocalOpenCommandForPlatformWindows(t *testing.T) {
	target := `C:\Users\Test\AppData\Roaming\pt-nexus\data\updates\downloads\installers\v4.0.2\pt-nexus-v4.0.2-amd64-update.exe`
	cmd := buildLocalOpenCommandForPlatform("windows", target)
	if cmd == nil {
		t.Fatal("expected command")
	}

	want := []string{"cmd.exe", "/c", "start", "", target}
	if len(cmd.Args) != len(want) {
		t.Fatalf("unexpected arg length: got=%d want=%d args=%v", len(cmd.Args), len(want), cmd.Args)
	}
	for i, item := range want {
		if cmd.Args[i] != item {
			t.Fatalf("unexpected arg[%d]: got=%q want=%q all=%v", i, cmd.Args[i], item, cmd.Args)
		}
	}
}

func TestBuildLocalOpenCommandForPlatformLinux(t *testing.T) {
	target := "/tmp/pt-nexus-update.exe"
	cmd := buildLocalOpenCommandForPlatform("linux", target)
	if cmd == nil {
		t.Fatal("expected command")
	}

	want := []string{"xdg-open", target}
	if len(cmd.Args) != len(want) {
		t.Fatalf("unexpected arg length: got=%d want=%d args=%v", len(cmd.Args), len(want), cmd.Args)
	}
	for i, item := range want {
		if cmd.Args[i] != item {
			t.Fatalf("unexpected arg[%d]: got=%q want=%q all=%v", i, cmd.Args[i], item, cmd.Args)
		}
	}
}

func TestBuildInstallerLaunchArgs(t *testing.T) {
	got := buildInstallerLaunchArgs(`C:\Program Files\pt-nexus`, 101, 202, 303)
	want := []string{
		`/PTNEXUS_INSTALL_DIR=C:\Program Files\pt-nexus`,
		"/PTNEXUS_MAIN_PID=101",
		"/PTNEXUS_SERVER_PID=202",
		"/PTNEXUS_UPDATER_PID=303",
	}

	if len(got) != len(want) {
		t.Fatalf("unexpected arg length: got=%d want=%d args=%v", len(got), len(want), got)
	}
	for i, item := range want {
		if got[i] != item {
			t.Fatalf("unexpected arg[%d]: got=%q want=%q all=%v", i, got[i], item, got)
		}
	}
}

func TestBuildInstallerLaunchArgsOmitZeroValues(t *testing.T) {
	got := buildInstallerLaunchArgs("", 101, 0, 0)
	want := []string{"/PTNEXUS_MAIN_PID=101"}

	if len(got) != len(want) {
		t.Fatalf("unexpected arg length: got=%d want=%d args=%v", len(got), len(want), got)
	}
	for i, item := range want {
		if got[i] != item {
			t.Fatalf("unexpected arg[%d]: got=%q want=%q all=%v", i, got[i], item, got)
		}
	}
}

func TestBuildInstallerLaunchCommand(t *testing.T) {
	path := `C:\Users\Test\Downloads\pt-nexus-v4.0.2-amd64-update.exe`
	args := []string{"/PTNEXUS_MAIN_PID=101"}
	cmd := buildInstallerLaunchCommand(path, args)
	if cmd == nil {
		t.Fatal("expected command")
	}

	want := []string{path, "/PTNEXUS_MAIN_PID=101"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("unexpected arg length: got=%d want=%d args=%v", len(cmd.Args), len(want), cmd.Args)
	}
	for i, item := range want {
		if cmd.Args[i] != item {
			t.Fatalf("unexpected arg[%d]: got=%q want=%q all=%v", i, cmd.Args[i], item, cmd.Args)
		}
	}
}
