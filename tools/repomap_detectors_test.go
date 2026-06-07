package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectPythonPackage_pyprojectName(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "pyproject.toml")
	if err := os.WriteFile(marker, []byte("[project]\nname = \"awesome-pkg\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mods, err := detectPythonPackage(dir, marker)
	if err != nil {
		t.Fatalf("detectPythonPackage: %v", err)
	}
	if len(mods) != 1 || mods[0].Type != "python-package" || mods[0].Name != "awesome-pkg" {
		t.Fatalf("unexpected modules: %+v", mods)
	}
}

func TestDetectPythonPackage_fallsBackToDirName(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "fallback-pkg")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// setup.py is also a valid marker; no pyproject.toml so name should
	// come from filepath.Base(dir).
	marker := filepath.Join(dir, "setup.py")
	if err := os.WriteFile(marker, []byte("from setuptools import setup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mods, err := detectPythonPackage(dir, marker)
	if err != nil {
		t.Fatalf("detectPythonPackage: %v", err)
	}
	if len(mods) != 1 || mods[0].Name != "fallback-pkg" {
		t.Fatalf("expected name from directory, got %+v", mods)
	}
}

func TestDetectPythonPackage_unreadablePyprojectFallsBackToDir(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "missing-toml")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Marker points at pyproject.toml that does not exist — exercises the
	// os.ReadFile error branch where name stays as the directory base.
	mods, err := detectPythonPackage(dir, filepath.Join(dir, "pyproject.toml"))
	if err != nil {
		t.Fatalf("detectPythonPackage: %v", err)
	}
	if len(mods) != 1 || mods[0].Name != "missing-toml" {
		t.Fatalf("expected directory fallback, got %+v", mods)
	}
}

func TestDetectMavenModule(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "svc-api")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mods, err := detectMavenModule(dir, filepath.Join(dir, "pom.xml"))
	if err != nil {
		t.Fatalf("detectMavenModule: %v", err)
	}
	if len(mods) != 1 || mods[0].Type != "maven-module" || mods[0].Name != "svc-api" {
		t.Fatalf("unexpected modules: %+v", mods)
	}
}

func TestDetectGradleModule(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "app")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mods, err := detectGradleModule(dir, filepath.Join(dir, "build.gradle"))
	if err != nil {
		t.Fatalf("detectGradleModule: %v", err)
	}
	if len(mods) != 1 || mods[0].Type != "gradle-module" || mods[0].Name != "app" {
		t.Fatalf("unexpected modules: %+v", mods)
	}
}

func TestDetectCMakeProject(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "engine")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mods, err := detectCMakeProject(dir, filepath.Join(dir, "CMakeLists.txt"))
	if err != nil {
		t.Fatalf("detectCMakeProject: %v", err)
	}
	if len(mods) != 1 || mods[0].Type != "cmake-project" || mods[0].Name != "engine" {
		t.Fatalf("unexpected modules: %+v", mods)
	}
}
