package deploytest

// The Rails image and the Rails asset pipeline. These read files that belong to
// Rails alone -- the root Dockerfile, package.json's esbuild/sass scripts and
// Procfile.dev -- and they go when those files go (#304). Until then they hold
// what they held in Ruby; each skips once its file is deleted, so deleting
// Rails is one change and not two.

import (
	"encoding/json"
	"strings"
	"testing"
)

// The allocator settings are four lines in a Dockerfile that nothing else in
// the suite looks at, and each of them was arrived at by measurement that
// contradicted the obvious choice (#148). A later edit that drops one would
// change how much memory production uses and break no test at all.
func TestAllocator(t *testing.T) {
	if !exists("Dockerfile") {
		t.Skip("the Rails image is gone, and its allocator with it")
	}
	// The instructions, without the prose: the Dockerfile explains at length
	// why MALLOC_ARENA_MAX is not set.
	d := directives(read(t, "Dockerfile"))

	t.Run("the runtime image installs jemalloc", func(t *testing.T) {
		mustMatch(t, `(?m)^\s+libjemalloc2\s*\\?$`, d, "libjemalloc2 is what LD_PRELOAD below resolves to")
	})
	t.Run("jemalloc is preloaded by bare soname", func(t *testing.T) {
		mustContain(t, d, "LD_PRELOAD=libjemalloc.so.2",
			"a bare soname lets the loader find it under whichever multiarch directory this architecture uses; a hard path would pin it to amd64")
	})
	// glibc 18.7 -> 18.5 MB; jemalloc with default settings 360 -> 345 MB;
	// jemalloc with these settings 316 -> 21 MB. Left alone, jemalloc purges a
	// dirty page only when something else allocates in the same arena.
	t.Run("jemalloc is told to give the pages back", func(t *testing.T) {
		mustMatch(t, `MALLOC_CONF=\S*background_thread:true`, d,
			"without a background thread the decay has no clock of its own, and jemalloc becomes a memory regression rather than a saving")
		mustMatch(t, `MALLOC_CONF=\S*dirty_decay_ms:\d+`, d, "no dirty decay")
		mustMatch(t, `MALLOC_CONF=\S*muzzy_decay_ms:\d+`, d, "no muzzy decay")
	})
	// Measured harmful, not merely redundant: glibc capped at two arenas
	// retained 123MB where the default retained 72MB.
	t.Run("the glibc arena cap is not reinstated", func(t *testing.T) {
		mustNotContain(t, d, "MALLOC_ARENA_MAX", "see the comment above LD_PRELOAD in the Dockerfile")
	})
	// LD_PRELOAD to a missing library is a no-op; the build has to be what notices.
	t.Run("the build refuses to produce an image where the preload does not take", func(t *testing.T) {
		mustContain(t, d, "RUN LD_PRELOAD=libjemalloc.so.2 ruby -e", "the build does not try the preload")
		mustMatch(t, `abort .*unless File\.read\("/proc/self/maps"\)\.include\?\("jemalloc"\)`, d, "the build does not refuse a preload that did not take")
	})
	// As an ENV it applies to every later RUN too.
	t.Run("the preload is set after the install that provides it", func(t *testing.T) {
		install, preload := strings.Index(d, "libjemalloc2"), strings.Index(d, "ENV LD_PRELOAD=")
		if install < 0 {
			t.Fatal("libjemalloc2 is not installed at all")
		}
		if preload < 0 {
			t.Fatal("LD_PRELOAD is not set at all")
		}
		if install > preload {
			t.Error("ENV LD_PRELOAD must come after the apt-get that installs the library")
		}
	})
}

// The build flags are four strings in package.json that no other test touches,
// and each was worth 100KB or more on the wire (#149).
func TestAssetBuild(t *testing.T) {
	if !exists("package.json") {
		t.Skip("the Rails asset pipeline is gone")
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(read(t, "package.json")), &pkg); err != nil {
		t.Fatal(err)
	}
	s := pkg.Scripts

	t.Run("the production JavaScript bundle is minified", func(t *testing.T) {
		mustContain(t, s["build"], "--minify", "unminified, application.js is 399KB against 183KB")
	})
	// 685KB of .map was being precompiled and shipped to production.
	t.Run("the production build emits no sourcemap", func(t *testing.T) {
		mustNotContain(t, s["build"], "--sourcemap", "a production sourcemap is precompiled and served to visitors")
	})
	// The trade runs the other way in development. Procfile.dev runs this.
	t.Run("development keeps its sourcemaps", func(t *testing.T) {
		mustContain(t, s["build:watch"], "--sourcemap", "development lost its sourcemaps")
		mustNotContain(t, s["build:watch"], "--minify", "development is minified")
		mustContain(t, read(t, "Procfile.dev"), "yarn build:watch", "bin/dev must run the build that keeps sourcemaps")
	})
	t.Run("the stylesheet is compressed", func(t *testing.T) {
		mustContain(t, s["build:css:compile"], "--style=compressed", "expanded, application.css is 1.2MB")
	})
	// sass was already told --no-source-map, and then postcss appended a
	// 625,688-byte base64 sourcemap of its own.
	t.Run("postcss does not append a sourcemap of its own", func(t *testing.T) {
		mustContain(t, s["build:css:prefix"], "--no-map", "postcss inlines a base64 sourcemap by default -- 625KB of it here")
	})
}
