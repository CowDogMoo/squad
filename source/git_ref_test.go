package source_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/source"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// fixtureRepo builds a local git repository with three commits, one tag,
// and a non-default branch so the checkout tests can exercise SHA-, tag-,
// and branch-style refs without any network access.
func fixtureRepo(t *testing.T) (path, firstSHA, tagName, branchName, branchSHA string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	sig := &object.Signature{Name: "Tester", Email: "t@example.com", When: time.Now()}

	write := func(name, body string) plumbing.Hash {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if _, err := wt.Add(name); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
		h, err := wt.Commit("commit "+name, &git.CommitOptions{Author: sig})
		if err != nil {
			t.Fatalf("commit %s: %v", name, err)
		}
		return h
	}

	first := write("a.txt", "one")
	_ = write("b.txt", "two")

	// Lightweight tag on the first commit.
	if _, err := repo.CreateTag("v0.1.0", first, nil); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	// Branch with its own commit.
	branchRef := plumbing.NewBranchReferenceName("topic")
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(branchRef, head.Hash())); err != nil {
		t.Fatalf("SetReference: %v", err)
	}
	if err := wt.Checkout(&git.CheckoutOptions{Branch: branchRef, Force: true}); err != nil {
		t.Fatalf("checkout topic: %v", err)
	}
	branchHead := write("c.txt", "three")

	// Return HEAD to default branch so clone gets it.
	defaultBranch := plumbing.NewBranchReferenceName("master")
	if err := wt.Checkout(&git.CheckoutOptions{Branch: defaultBranch, Force: true}); err != nil {
		t.Fatalf("checkout master: %v", err)
	}

	return dir, first.String(), "v0.1.0", "topic", branchHead.String()
}

func cloneFromFixture(t *testing.T, fixtureDir string) (cacheDir string, gitURL string, ops *source.GitOperations) {
	t.Helper()
	cacheDir = t.TempDir()
	ops = source.NewGitOperations(cacheDir)
	gitURL = "file://" + fixtureDir
	return cacheDir, gitURL, ops
}

func headHash(t *testing.T, repoPath string) string {
	t.Helper()
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	return head.Hash().String()
}

func TestCloneOrUpdate_emptyRefLeavesDefaultBranchHEAD(t *testing.T) {
	t.Parallel()
	fixture, _, _, _, _ := fixtureRepo(t)
	_, url, ops := cloneFromFixture(t, fixture)

	repoPath, err := ops.CloneOrUpdate(url, "")
	if err != nil {
		t.Fatalf("CloneOrUpdate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoPath, "b.txt")); err != nil {
		t.Fatalf("default branch tip should include b.txt: %v", err)
	}
}

func TestCloneOrUpdate_checksOutSHA(t *testing.T) {
	t.Parallel()
	fixture, firstSHA, _, _, _ := fixtureRepo(t)
	_, url, ops := cloneFromFixture(t, fixture)

	repoPath, err := ops.CloneOrUpdate(url, firstSHA)
	if err != nil {
		t.Fatalf("CloneOrUpdate(SHA): %v", err)
	}
	if got := headHash(t, repoPath); got != firstSHA {
		t.Fatalf("HEAD = %s, want %s", got, firstSHA)
	}
	if _, err := os.Stat(filepath.Join(repoPath, "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("first commit shouldn't have b.txt; got err=%v", err)
	}
}

func TestCloneOrUpdate_checksOutTag(t *testing.T) {
	t.Parallel()
	fixture, firstSHA, tag, _, _ := fixtureRepo(t)
	_, url, ops := cloneFromFixture(t, fixture)

	repoPath, err := ops.CloneOrUpdate(url, tag)
	if err != nil {
		t.Fatalf("CloneOrUpdate(tag): %v", err)
	}
	if got := headHash(t, repoPath); got != firstSHA {
		t.Fatalf("tag %q resolved to %s, want %s", tag, got, firstSHA)
	}
}

func TestCloneOrUpdate_checksOutRemoteBranch(t *testing.T) {
	t.Parallel()
	fixture, _, _, branch, branchSHA := fixtureRepo(t)
	_, url, ops := cloneFromFixture(t, fixture)

	repoPath, err := ops.CloneOrUpdate(url, branch)
	if err != nil {
		t.Fatalf("CloneOrUpdate(branch): %v", err)
	}
	if got := headHash(t, repoPath); got != branchSHA {
		t.Fatalf("branch %q resolved to %s, want %s", branch, got, branchSHA)
	}
	if _, err := os.Stat(filepath.Join(repoPath, "c.txt")); err != nil {
		t.Fatalf("topic branch tip should include c.txt: %v", err)
	}
}

func TestCloneOrUpdate_unknownRefIsError(t *testing.T) {
	t.Parallel()
	fixture, _, _, _, _ := fixtureRepo(t)
	_, url, ops := cloneFromFixture(t, fixture)

	if _, err := ops.CloneOrUpdate(url, "does-not-exist"); err == nil {
		t.Fatal("expected error for unknown ref")
	}
}

func TestCloneOrUpdate_corruptCacheDirErrors(t *testing.T) {
	t.Parallel()
	fixture, _, _, _, _ := fixtureRepo(t)
	cacheDir, url, ops := cloneFromFixture(t, fixture)

	// First clone succeeds.
	repoPath, err := ops.CloneOrUpdate(url, "")
	if err != nil {
		t.Fatalf("initial CloneOrUpdate: %v", err)
	}

	// Corrupt the cache by removing the .git dir; subsequent calls with
	// a non-empty ref should surface a PlainOpen error from checkoutRef.
	if err := os.RemoveAll(filepath.Join(repoPath, ".git")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, err := ops.CloneOrUpdate(url, "v0.1.0"); err == nil {
		t.Fatal("expected PlainOpen failure on corrupt cache dir")
	}
	_ = cacheDir
}

func TestCloneOrUpdate_followupUpdateSwitchesRef(t *testing.T) {
	t.Parallel()
	fixture, firstSHA, tag, _, _ := fixtureRepo(t)
	_, url, ops := cloneFromFixture(t, fixture)

	// First clone tracks the default branch.
	if _, err := ops.CloneOrUpdate(url, ""); err != nil {
		t.Fatalf("initial CloneOrUpdate: %v", err)
	}
	// Subsequent CloneOrUpdate with a ref must rewind to that ref.
	repoPath, err := ops.CloneOrUpdate(url, tag)
	if err != nil {
		t.Fatalf("CloneOrUpdate(tag) on existing clone: %v", err)
	}
	if got := headHash(t, repoPath); got != firstSHA {
		t.Fatalf("after switching to tag, HEAD = %s, want %s", got, firstSHA)
	}
}
