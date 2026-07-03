package runner

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	wappalyzer "github.com/projectdiscovery/wappalyzergo"
	"github.com/sensepost/gowitness/internal/islazy"
	"github.com/sensepost/gowitness/pkg/models"
	"github.com/sensepost/gowitness/pkg/prefilter"
	"github.com/sensepost/gowitness/pkg/writers"
)

// Runner is a runner that probes web targets using a driver
type Runner struct {
	Driver     Driver
	Wappalyzer *wappalyzer.Wappalyze

	// options for the Runner to consider
	options Options
	// writers are the result writers to use
	writers []writers.Writer
	// log handler
	log *slog.Logger

	// Targets to scan.
	// This would typically be fed from a gowitness/pkg/reader.
	Targets chan string

	// LiveTargets holds targets that survived the TCP liveness pre-filter.
	// When prefiltering is enabled, Chrome workers consume from this channel
	// instead of Targets.
	LiveTargets chan string

	// in case we need to bail
	ctx    context.Context
	cancel context.CancelFunc
}

// New gets a new Runner ready for probing.
// It's up to the caller to call Close() on the runner
func NewRunner(logger *slog.Logger, driver Driver, opts Options, writers []writers.Writer) (*Runner, error) {
	if !opts.Scan.ScreenshotSkipSave {
		screenshotPath, err := islazy.CreateDir(opts.Scan.ScreenshotPath)
		if err != nil {
			return nil, err
		}
		opts.Scan.ScreenshotPath = screenshotPath
		logger.Debug("final screenshot path", "screenshot-path", opts.Scan.ScreenshotPath)
	} else {
		logger.Debug("not saving screenshots to disk")
	}

	// screenshot format check
	if !islazy.SliceHasStr([]string{"jpeg", "png"}, opts.Scan.ScreenshotFormat) {
		return nil, errors.New("invalid screenshot format")
	}

	// javascript file containing javascript to eval on each page.
	// just read it in and set Scan.JavaScript to the value.
	if opts.Scan.JavaScriptFile != "" {
		javascript, err := os.ReadFile(opts.Scan.JavaScriptFile)
		if err != nil {
			return nil, err
		}

		opts.Scan.JavaScript = string(javascript)
	}

	// get a wappalyzer instance
	wap, err := wappalyzer.New()
	if err != nil {
		return nil, err
	}

	// auto-tune the worker count when not explicitly set (Threads <= 0).
	// witness work is I/O-bound (network + chrome), so oversubscribe cores.
	if opts.Scan.Threads <= 0 {
		n := runtime.NumCPU()
		if n < 8 {
			n = 8
		}
		if n > 32 {
			n = 32
		}
		opts.Scan.Threads = n
		logger.Debug("auto-tuned threads", "threads", opts.Scan.Threads, "num-cpu", runtime.NumCPU())
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Runner{
		Driver:      driver,
		Wappalyzer:  wap,
		options:     opts,
		writers:     writers,
		Targets:     make(chan string, 1000),
		LiveTargets: make(chan string, 2*opts.Scan.Threads),
		log:         logger,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// runWriters takes a result and passes it to writers
func (run *Runner) runWriters(result *models.Result) error {
	for _, writer := range run.writers {
		if err := writer.Write(result); err != nil {
			return err
		}
	}

	return nil
}

// checkUrl ensures a url is valid
func (run *Runner) checkUrl(target string) error {
	url, err := url.ParseRequestURI(target)
	if err != nil {
		return err
	}

	if !islazy.SliceHasStr(run.options.Scan.UriFilter, url.Scheme) {
		return errors.New("url contains invalid scheme")
	}

	return nil
}

// Run executes the runner, processing targets as they arrive
// in the Targets channel
func (run *Runner) Run() {
	// witnessSource is the channel Chrome workers consume from. With the
	// liveness pre-filter enabled, a cheap TCP-dial stage sits between the
	// reader (Targets) and the (expensive) Chrome workers (LiveTargets),
	// so dead hosts never reach Chrome.
	witnessSource := run.Targets

	// The pre-filter dials targets directly with net.DialTimeout, bypassing any
	// Chrome proxy. If a proxy is configured (e.g. to reach internal-only hosts
	// or via SOCKS), a direct dial would fail for proxy-reachable-only targets
	// and wrongly drop them as dead — so disable pre-filtering in that case.
	if run.options.Scan.Prefilter && run.options.Chrome.Proxy != "" {
		run.log.Warn("liveness pre-filter disabled because a Chrome proxy is set (direct TCP dial would bypass the proxy)")
	} else if run.options.Scan.Prefilter {
		witnessSource = run.LiveTargets
		run.startPrefilter()
	}

	wg := sync.WaitGroup{}

	// will spawn Scan.Theads number of "workers" as goroutines
	for w := 0; w < run.options.Scan.Threads; w++ {
		wg.Add(1)

		// start a worker
		go func() {
			defer wg.Done()
			for {
				select {
				case <-run.ctx.Done():
					return
				case target, ok := <-witnessSource:
					if !ok {
						return
					}

					// validate the target
					if err := run.checkUrl(target); err != nil {
						if run.options.Logging.LogScanErrors {
							run.log.Error("invalid target to scan", "target", target, "err", err)
						}
						continue
					}

					result, err := run.Driver.Witness(target, run)
					if err != nil {
						// is this a chrome not found error?
						var chromeErr *ChromeNotFoundError
						if errors.As(err, &chromeErr) {
							run.log.Error("no valid chrome installation found", "err", err)
							run.cancel()
							return
						}

						if run.options.Logging.LogScanErrors {
							run.log.Error("failed to witness target", "target", target, "err", err)
						}
						continue
					}

					// assume that status code 0 means there was no information, so
					// don't send anything to writers.
					if result.ResponseCode == 0 {
						if run.options.Logging.LogScanErrors {
							run.log.Error("failed to witness target, status code was 0", "target", target)
						}
						continue
					}

					if err := run.runWriters(result); err != nil {
						run.log.Error("failed to write result for target", "target", target, "err", err)
					}

					run.log.Info("result 🤖", "target", target, "status-code", result.ResponseCode,
						"title", result.Title, "have-screenshot", !result.Failed)

				}
			}

		}()
	}

	wg.Wait()
}

// startPrefilter launches a pool of cheap TCP-dial workers that consume from
// run.Targets and forward only live targets to run.LiveTargets. Dead targets
// (NXDOMAIN/refused/timeout) are dropped here so they never reach Chrome.
// run.LiveTargets is closed once every target has been pre-checked.
func (run *Runner) startPrefilter() {
	// pre-filter network dials are far cheaper than Chrome, so fan out wider
	// than the Chrome worker pool to keep the live pipeline saturated.
	workers := run.options.Scan.Threads * 2
	if workers > 64 {
		workers = 64
	}
	if workers < 1 {
		workers = 1
	}

	timeout := time.Duration(run.options.Scan.PrefilterTimeout) * time.Second
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	var live, dead atomic.Int64

	go func() {
		wg := sync.WaitGroup{}
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-run.ctx.Done():
						return
					case target, ok := <-run.Targets:
						if !ok {
							return
						}
						if prefilter.IsLive(target, timeout) {
							live.Add(1)
							select {
							case run.LiveTargets <- target:
							case <-run.ctx.Done():
								return
							}
						} else {
							dead.Add(1)
							if run.options.Logging.LogScanErrors {
								run.log.Debug("prefilter dropped dead target", "target", target)
							}
						}
					}
				}
			}()
		}
		wg.Wait()
		close(run.LiveTargets)
		run.log.Info("liveness pre-filter complete", "live", live.Load(), "dead", dead.Load(),
			"dial-timeout", timeout.String(), "workers", workers)
	}()
}

func (run *Runner) Close() {
	// close the driver
	run.Driver.Close()
}
