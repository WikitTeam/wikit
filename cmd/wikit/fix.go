package main

import (
	"flag"
	"fmt"
	"os"

	"wikit/internal/config"
	"wikit/internal/fixarchive"
	"wikit/internal/wiki"
)

// fixOpts holds the flags "wikit fixpage" accepts. It reuses the backup config
// plumbing so the archive location is resolved exactly the way a backup would:
// config.json's base_directory, overridable with --base-dir.
type fixOpts struct {
	configPath string
	configSet  bool
	baseDir    *string
	dryRun     bool
}

func parseFixArgs(args []string) (fixOpts, []string, error) {
	var opts fixOpts

	defaultConfig := os.Getenv("WIKIT_CONFIG")
	if defaultConfig == "" {
		defaultConfig = "config.json"
	}

	fs := flag.NewFlagSet("fixpage", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&opts.configPath, "config", defaultConfig, "config file path")
	fs.StringVar(&opts.configPath, "c", defaultConfig, "config file path (shorthand)")
	baseDir := fs.String("base-dir", "", "override base_directory")
	dryRun := fs.Bool("dry-run", false, "report what would be repaired without writing")

	// Same interleaving of flags and positional names as backup.
	var targets []string
	rest := args
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return opts, nil, err
		}
		rest = fs.Args()
		if len(rest) > 0 {
			targets = append(targets, rest[0])
			rest = rest[1:]
		}
	}

	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })
	if setFlags["base-dir"] {
		opts.baseDir = baseDir
	}
	opts.dryRun = *dryRun
	opts.configSet = setFlags["config"] || setFlags["c"]

	return opts, targets, nil
}

// runFix repairs archives written by older wikit builds, which baked the
// relative work-directory prefix into every member name.
func runFix(args []string) error {
	opts, targets, err := parseFixArgs(args)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("nothing to repair: specify 'all' or one or more wiki names")
	}

	cfg, err := loadConfigOrDefault(backupOpts{
		configPath: opts.configPath,
		configSet:  opts.configSet,
		baseDir:    opts.baseDir,
	}, targets)
	if err != nil {
		return err
	}
	if opts.baseDir != nil {
		cfg.BaseDirectory = *opts.baseDir
	}
	if cfg.BaseDirectory == "" {
		return fmt.Errorf("base_directory is not set")
	}

	names, err := resolveFixTargets(cfg, targets)
	if err != nil {
		return err
	}

	var total fixarchive.Result
	var failedWikis []string
	for _, name := range names {
		dir := cfg.BaseDirectory + "/" + name
		res, err := fixarchive.Wiki(dir, fixarchive.Options{
			DryRun: opts.dryRun,
			Log:    func(level, msg string) { wiki.Log(level, name, msg) },
		})
		if err != nil {
			wiki.LogError(name, err.Error())
			failedWikis = append(failedWikis, name)
			continue
		}
		total.Add(res)
		verb := "repaired"
		if opts.dryRun {
			verb = "would repair"
		}
		wiki.Log("INFO", name, fmt.Sprintf("%d archives scanned, %s %d, already correct %d, failed %d",
			res.Scanned, verb, res.Fixed, res.OK, res.Failed))
	}

	if len(names) > 1 {
		fmt.Printf("total: %d archives scanned, %d need repair, %d already correct, %d failed\n",
			total.Scanned, total.Fixed, total.OK, total.Failed)
	}
	if len(failedWikis) > 0 {
		return fmt.Errorf("could not repair: %v", failedWikis)
	}
	if total.Failed > 0 {
		return fmt.Errorf("%d archives could not be repaired", total.Failed)
	}
	return nil
}

// resolveFixTargets expands "all" to the configured wiki names. Unlike backup,
// no URL is needed: a name only has to map to <base_directory>/<name>.
func resolveFixTargets(cfg *config.Config, targets []string) ([]string, error) {
	for _, t := range targets {
		if t == "all" {
			if len(targets) != 1 {
				return nil, fmt.Errorf("'all' cannot be combined with explicit wiki names")
			}
			if len(cfg.Wikis) == 0 {
				return nil, fmt.Errorf("config lists no wikis")
			}
			out := make([]string, len(cfg.Wikis))
			for i, w := range cfg.Wikis {
				out[i] = w.Name
			}
			return out, nil
		}
	}
	return targets, nil
}
