package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/bnkrr/rses/internal/buildinfo"
	"github.com/bnkrr/rses/internal/daemon"
	"github.com/bnkrr/rses/internal/initializer"
	"github.com/bnkrr/rses/internal/selector"
	"github.com/bnkrr/rses/internal/session"
	"github.com/bnkrr/rses/internal/tui"
)

func Run(args []string) error {
	dir, args, err := parseGlobalOptions(args)
	if err != nil {
		return err
	}
	previousDir := configDirectory
	configDirectory = dir
	defer func() { configDirectory = previousDir }()

	if len(args) == 0 {
		usage(os.Stdout)
		return nil
	}

	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		usage(os.Stdout)
		return nil
	}
	if args[0] == "--version" {
		return version(args[1:])
	}
	for _, cmd := range commands {
		if cmd.name == args[0] {
			return cmd.run(args[1:])
		}
	}
	return fmt.Errorf("unknown command %q\n\nRun `rses help` for usage.", args[0])
}

type commandSpec struct {
	name string
	run  func([]string) error
}

var commands = []commandSpec{
	{name: "init", run: initCommand},
	{name: "version", run: version},
	{name: "daemon", run: daemonCommand},
	{name: "list", run: list},
	{name: "start", run: start},
	{name: "stop", run: stop},
	{name: "status", run: status},
	{name: "logs", run: logs},
	{name: "explain", run: explain},
	{name: "watch", run: watch},
}

func usage(w io.Writer) {
	fmt.Fprint(w, `rses manages project-defined remote sessions.

Usage:
  rses [-C DIR] COMMAND
  rses init [DIR]
  rses version
  rses list [--remote] [--host HOST] [--group GROUP|--tag TAG...] [--wide] [NAME...]
  rses start [--remote] [--host HOST] [--all|--group GROUP|--tag TAG...] [--watch|--no-watch] [--parallel N] [NAME...]
  rses stop [--remote] [--host HOST] [--all|--group GROUP|--tag TAG...] [--parallel N] [NAME...]
  rses status [--remote] [--host HOST] [--group GROUP|--tag TAG...] [--parallel N] [--wide] [NAME...]
  rses logs [--remote] [--follow] NAME
  rses explain [--remote] [--host HOST] NAME
  rses watch [--remote] [--host HOST] [--group GROUP|--tag TAG...] [NAME...]
  rses daemon [meta|shutdown]
`)
}

func parseGlobalOptions(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", args, nil
	}
	if args[0] == "-C" {
		if len(args) < 2 || args[1] == "" {
			return "", nil, fmt.Errorf("-C requires a directory")
		}
		return args[1], args[2:], nil
	}
	if len(args[0]) >= 3 && args[0][:3] == "-C=" {
		if len(args[0]) == 3 {
			return "", nil, fmt.Errorf("-C requires a directory")
		}
		return args[0][3:], args[1:], nil
	}
	return "", args, nil
}

func initCommand(args []string) error {
	if configDirectory != "" {
		return fmt.Errorf("-C is not valid with init; pass the target directory to `rses init [DIR]`")
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: rses init [DIR]")
	}
	target := ""
	var err error
	if len(args) == 1 {
		target = args[0]
	} else {
		target, err = initializer.DefaultDir()
		if err != nil {
			return err
		}
	}
	initialized, err := initializer.Initialize(target)
	if err != nil {
		return err
	}
	fmt.Printf("initialized rses configuration at %s\n", initialized)
	return nil
}

func version(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: rses version")
	}
	fmt.Printf("rses %s\n", buildinfo.Current())
	return nil
}

func daemonCommand(args []string) error {
	if len(args) == 0 {
		return daemon.RunForeground()
	}
	switch args[0] {
	case "meta":
		if len(args) != 1 {
			return fmt.Errorf("usage: rses daemon meta")
		}
		if err := daemon.EnsureRunning(); err != nil {
			return err
		}
		resp, err := daemon.Call(daemon.Request{Action: "meta"})
		if err != nil {
			return err
		}
		if resp.Meta == nil {
			return fmt.Errorf("daemon returned no metadata")
		}
		fmt.Printf("protocol_version: %d\n", resp.Meta.ProtocolVersion)
		fmt.Printf("build_id: %s\n", resp.Meta.BuildID)
		fmt.Printf("exe_path: %s\n", resp.Meta.ExePath)
		fmt.Printf("pid: %d\n", resp.Meta.PID)
		fmt.Printf("started_at: %s\n", resp.Meta.StartedAt.Format(time.RFC3339))
		return nil
	case "shutdown":
		if len(args) != 1 {
			return fmt.Errorf("usage: rses daemon shutdown")
		}
		_, err := daemon.Call(daemon.Request{Action: "shutdown"})
		return err
	default:
		return fmt.Errorf("unknown daemon subcommand %q", args[0])
	}
}

func list(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	group := fs.String("group", "", "group")
	var tags tagValues
	fs.Var(&tags, "tag", "tag")
	includeRemote := fs.Bool("remote", false, "include remote sessions")
	host := fs.String("host", "", "remote host")
	wide := fs.Bool("wide", false, "wide output")
	if err := parseInterspersed(fs, args, "group", "tag", "host", "wide"); err != nil {
		return err
	}
	if *host != "" && !*includeRemote {
		return fmt.Errorf("use --remote with --host")
	}

	projectDir, all, _, err := load()
	if err != nil {
		return err
	}
	_ = projectDir
	specs := selector.Select(all, fs.Args(), *group, tags.Values(), *includeRemote, *host)
	printSpecs(specs, *wide)
	return nil
}

func start(args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	allFlag := fs.Bool("all", false, "start all sessions")
	group := fs.String("group", "", "group")
	var tags tagValues
	fs.Var(&tags, "tag", "tag")
	includeRemote := fs.Bool("remote", false, "include remote sessions")
	host := fs.String("host", "", "remote host")
	watchFlag := fs.Bool("watch", false, "enter watch")
	noWatch := fs.Bool("no-watch", false, "do not enter watch")
	parallel := fs.Int("parallel", 0, "parallel operations")
	if err := parseInterspersed(fs, args, "group", "tag", "host", "parallel"); err != nil {
		return err
	}
	if *host != "" && !*includeRemote {
		return fmt.Errorf("use --remote with --host")
	}

	projectDir, all, runtime, err := load()
	if err != nil {
		return err
	}
	specs, err := selector.SelectStartStop(all, fs.Args(), *group, tags.Values(), *allFlag, "start", *includeRemote, *host)
	if err != nil {
		return err
	}
	specs, err = selector.ExpandStartDependencies(specs, all, *includeRemote, *host)
	if err != nil {
		return err
	}
	if err := daemon.EnsureRunning(); err != nil {
		return err
	}
	_, err = streamOperation(daemon.Request{Action: "start", ProjectDir: projectDir, Specs: specs, Parallel: *parallel, Runtime: runtime}, printOperationEvent)
	if err != nil {
		return err
	}

	shouldWatch := *watchFlag || ((*group != "" || tags.HasAny() || len(specs) > 1) && !*noWatch)
	if shouldWatch {
		return tui.Run(projectDir, all, specs, runtime)
	}
	return nil
}

func stop(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	allFlag := fs.Bool("all", false, "stop all sessions")
	group := fs.String("group", "", "group")
	var tags tagValues
	fs.Var(&tags, "tag", "tag")
	includeRemote := fs.Bool("remote", false, "include remote sessions")
	host := fs.String("host", "", "remote host")
	parallel := fs.Int("parallel", 0, "parallel operations")
	if err := parseInterspersed(fs, args, "group", "tag", "host", "parallel"); err != nil {
		return err
	}
	if *host != "" && !*includeRemote {
		return fmt.Errorf("use --remote with --host")
	}

	projectDir, all, runtime, hasConfig, err := loadOptional()
	if err != nil {
		return err
	}
	specs, err := selector.SelectStartStopOptional(all, fs.Args(), *group, tags.Values(), *allFlag, hasConfig, "stop", *includeRemote, *host)
	if err != nil {
		return err
	}
	specs, err = selector.AppendStopDependencies(specs, all, *includeRemote, *host)
	if err != nil {
		return err
	}
	if err := daemon.EnsureRunning(); err != nil {
		return err
	}
	req := daemon.Request{
		Action:            "stop",
		ProjectDir:        selector.ProjectDirForRequest(projectDir, specs, hasConfig, fs.Args(), *group, tags.Values(), *allFlag),
		Specs:             specs,
		Names:             selector.NamesForRequest(fs.Args(), *allFlag),
		Group:             *group,
		Tags:              tags.Values(),
		Host:              *host,
		IncludeConfigured: hasConfig,
		Runtime:           runtime,
	}
	req.Parallel = *parallel
	summary, err := streamOperation(req, printOperationEvent)
	if err != nil {
		return err
	}
	if len(summary.Results) == 0 {
		return fmt.Errorf("no running or configured sessions matched")
	}
	return nil
}

func status(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	group := fs.String("group", "", "group")
	var tags tagValues
	fs.Var(&tags, "tag", "tag")
	includeRemote := fs.Bool("remote", false, "include remote sessions")
	host := fs.String("host", "", "remote host")
	parallel := fs.Int("parallel", 0, "parallel operations")
	wide := fs.Bool("wide", false, "wide output")
	if err := parseInterspersed(fs, args, "group", "tag", "host", "parallel", "wide"); err != nil {
		return err
	}
	if *host != "" && !*includeRemote {
		return fmt.Errorf("use --remote with --host")
	}

	projectDir, all, runtime, hasConfig, err := loadOptional()
	if err != nil {
		return err
	}
	var specs []session.Spec
	if hasConfig {
		specs = selector.Select(all, fs.Args(), *group, tags.Values(), *includeRemote, *host)
	}
	if err := daemon.EnsureRunning(); err != nil {
		return err
	}
	printStatusHeader(*wide)
	summary, err := streamOperation(daemon.Request{
		Action:            "status",
		ProjectDir:        selector.ProjectDirForRequest(projectDir, specs, hasConfig, fs.Args(), *group, tags.Values(), false),
		Specs:             specs,
		Names:             fs.Args(),
		Group:             *group,
		Tags:              tags.Values(),
		Host:              *host,
		IncludeConfigured: hasConfig,
		Parallel:          *parallel,
		Runtime:           runtime,
	}, func(event daemon.OperationEvent) {
		if event.Type == "status" && event.Status != nil {
			printStatusRow(*event.Status, *wide)
		}
	})
	if err != nil {
		return err
	}
	if len(summary.Statuses) == 0 {
		return fmt.Errorf("no running or configured sessions matched")
	}
	return nil
}

func logs(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	follow := fs.Bool("follow", false, "follow")
	includeRemote := fs.Bool("remote", false, "include remote sessions")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if len(fs.Args()) != 1 {
		return fmt.Errorf("usage: rses logs [--remote] [--follow] NAME")
	}
	name := fs.Args()[0]
	projectDir, all, runtime, hasConfig, err := loadOptional()
	if err != nil {
		return err
	}
	var specs []session.Spec
	if hasConfig {
		specs = selector.Select(all, []string{name}, "", nil, *includeRemote, "")
		if len(specs) == 0 && !*includeRemote {
			if remote := selector.Select(all, []string{name}, "", nil, true, ""); len(remote) > 0 && remote[0].IsRemote() {
				return fmt.Errorf("remote session %q requires --remote", name)
			}
		}
	}
	if len(specs) == 1 && specs[0].IsRemote() {
		if *follow {
			return fmt.Errorf("remote logs do not support --follow yet")
		}
		if err := daemon.EnsureRunning(); err != nil {
			return err
		}
		resp, err := daemon.Call(daemon.Request{Action: "logs", ProjectDir: projectDir, Specs: specs, Lines: 200, Runtime: runtime})
		if err != nil {
			return err
		}
		_, err = io.WriteString(os.Stdout, resp.Log)
		return err
	}
	if err := daemon.EnsureRunning(); err != nil {
		return err
	}
	statuses, err := collectStatuses(daemon.Request{
		Action:            "status",
		ProjectDir:        selector.ProjectDirForRequest(projectDir, specs, hasConfig, []string{name}, "", nil, false),
		Specs:             specs,
		Names:             []string{name},
		IncludeConfigured: hasConfig,
		Runtime:           runtime,
	})
	if err != nil {
		return err
	}
	if len(statuses) == 0 || statuses[0].LogPath == "" {
		return fmt.Errorf("no log path for %q", name)
	}
	if *follow {
		return followFile(statuses[0].LogPath, os.Stdout)
	}
	data, err := os.ReadFile(statuses[0].LogPath)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(data)
	return err
}

func explain(args []string) error {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	includeRemote := fs.Bool("remote", false, "include remote sessions")
	host := fs.String("host", "", "remote host")
	if err := parseInterspersed(fs, args, "host"); err != nil {
		return err
	}
	if *host != "" && !*includeRemote {
		return fmt.Errorf("use --remote with --host")
	}
	if len(fs.Args()) != 1 {
		return fmt.Errorf("usage: rses explain [--remote] [--host HOST] NAME")
	}
	name := fs.Args()[0]
	projectDir, all, _, err := load()
	if err != nil {
		return err
	}
	specs := selector.Select(all, []string{name}, "", nil, *includeRemote, *host)
	if len(specs) == 0 {
		if remoteSpecs := selector.Select(all, []string{name}, "", nil, true, ""); len(remoteSpecs) > 0 && remoteSpecs[0].IsRemote() && !*includeRemote {
			return fmt.Errorf("remote session %q requires --remote", name)
		}
		return fmt.Errorf("no sessions matched")
	}
	spec := specs[0]
	printExplain(os.Stdout, projectDir, spec)
	return nil
}

func watch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	group := fs.String("group", "", "group")
	var tags tagValues
	fs.Var(&tags, "tag", "tag")
	includeRemote := fs.Bool("remote", false, "include remote sessions")
	host := fs.String("host", "", "remote host")
	if err := parseInterspersed(fs, args, "group", "tag", "host"); err != nil {
		return err
	}
	if *host != "" && !*includeRemote {
		return fmt.Errorf("use --remote with --host")
	}
	projectDir, all, runtime, hasConfig, err := loadOptional()
	if err != nil {
		return err
	}
	var selected []session.Spec
	if hasConfig {
		selected = selector.Select(all, fs.Args(), *group, tags.Values(), *includeRemote, *host)
	} else {
		if err := daemon.EnsureRunning(); err != nil {
			return err
		}
		statuses, err := collectStatuses(daemon.Request{Action: "status", Names: fs.Args(), Group: *group, Tags: tags.Values(), Host: *host, Runtime: runtime})
		if err != nil {
			return err
		}
		selected = specsFromStatuses(statuses)
	}
	if len(selected) == 0 {
		return fmt.Errorf("no sessions matched")
	}
	if err := daemon.EnsureRunning(); err != nil {
		return err
	}
	return tui.Run(projectDir, all, selected, runtime)
}
