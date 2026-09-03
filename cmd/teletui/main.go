package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/app"
	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/version"
)

func main() {
	// Before anything else, including the debug log and the config: a
	// binary has to be able to say what it is on a machine where the rest
	// of it will not start.
	if version.Asked(os.Args[1:]) {
		fmt.Println(version.String("tele-tui"))
		return
	}

	// The TUI owns the terminal in raw mode, so any stray write to stderr
	// lands in the middle of a rendered frame — the Telegram client logs
	// "connection state: connecting" from goroutines its constructor starts,
	// which littered the screen whenever the network was down. Logging is
	// therefore discarded by default and this must run before anything that
	// logs is constructed.
	//
	// Set TELETUI_DEBUG=/tmp/teletui.log to redirect the log to a file
	// instead. Only this binary is silenced; the telegram-api and MCP
	// binaries keep logging to stderr.
	log.SetOutput(io.Discard)
	if dbg := os.Getenv("TELETUI_DEBUG"); dbg != "" {
		// O_APPEND, not O_TRUNC: debugging this app usually means running
		// it repeatedly, and truncating on every start destroys the log of
		// the run that actually reproduced the problem. A session banner
		// keeps successive runs tellable apart.
		f, err := os.OpenFile(dbg, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			// Loud, because the user explicitly asked for a debug log and
			// would otherwise sit waiting for output that never arrives.
			// Safe to print: this runs before the alt screen is entered.
			fmt.Fprintf(os.Stderr,
				"teletui: cannot open TELETUI_DEBUG log %q: %v\n"+
					"teletui: continuing with logging disabled\n", dbg, err)
		} else {
			log.SetOutput(f)
			defer f.Close()
			fmt.Fprintf(f, "\n=== teletui session started %s (pid %d) ===\n",
				time.Now().Format(time.RFC3339), os.Getpid())
		}
	}

	migrate := flag.Bool("migrate-config", false,
		"rewrite config.toml with the current default keybindings and exit")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fatalf("Failed to load config: %v", err)
	}

	if *migrate {
		runMigrateConfig(cfg)
		return
	}

	if cfg.Telegram.APIID == 0 || cfg.Telegram.APIHash == "" {
		if err := setupWizard(cfg); err != nil {
			fatalf("Setup failed: %v", err)
		}
	}

	s := store.NewStore()
	authorizer := telegram.NewTUIAuthorizer(cfg)

	// Construct first: callbacks and the update dispatcher must be fully wired
	// before Telegram can authenticate or replay an offline update gap.
	tgClient := telegram.NewClient(cfg, authorizer)

	// Create root model.
	model := app.New(cfg, tgClient, s, authorizer)

	// Create bubbletea program.
	p := tea.NewProgram(model)

	// Wire auth state changes into bubbletea via p.Send().
	authorizer.SetStateCallback(func(state telegram.AuthState, hint string) {
		p.Send(app.AuthStateChangedMsg{State: int(state), Hint: hint})
	})
	authorizer.SetErrorCallback(func(err error) {
		p.Send(app.AuthErrorMsg{Err: err})
	})
	listener, err := telegram.NewListener(tgClient, p)
	if err != nil {
		_ = tgClient.Close()
		fatalf("Failed to register Telegram updates: %v", err)
	}
	// Program.Send blocks until p.Run starts. Start from a goroutine so any
	// construction-time notices can wait for the event loop without keeping
	// the main goroutine from reaching it.
	go func() {
		if err := listener.Start(); err != nil {
			p.Send(app.AuthErrorMsg{Err: fmt.Errorf("start Telegram client: %w", err)})
		}
	}()

	// Once client is ready, load the authenticated account identity.
	go func() {
		tgClient.WaitReady()
		// Notify TUI that we're authenticated. Retry a few times — the
		// connection may still be settling right after auth.
		var me *telegram.User
		var err error
		for attempt := 0; attempt < 5; attempt++ {
			me, err = tgClient.GetMe()
			if err == nil && me != nil {
				break
			}
			log.Printf("get me attempt %d failed: %v", attempt+1, err)
			time.Sleep(time.Second)
		}
		if me == nil {
			p.Send(app.AuthErrorMsg{Err: fmt.Errorf("failed to load account info: %w", err)})
			return
		}
		p.Send(app.AuthenticatedMsg{
			UserId:    me.ID,
			FirstName: me.FirstName,
			LastName:  me.LastName,
		})
	}()

	// Clipboard spool files are intentionally not cleaned up here.
	// Bubble Tea v2 never waits for in-flight tea.Cmd goroutines on quit, so
	// deleting the spool dir at exit could race an upload still reading from
	// it. The next process to run sweeps up this one's spool directory
	// instead (see internal/clipboard).
	if _, err := p.Run(); err != nil {
		_ = tgClient.Close()
		fatalf("Error running TUI: %v", err)
	}

	if err := tgClient.Close(); err != nil {
		// The TUI session itself completed successfully and Bubble Tea has
		// already restored the terminal. A bounded backend unwind timing out is
		// diagnostic, not a reason to turn that clean session into exit code 1.
		log.Printf("Telegram client shutdown did not complete cleanly: %v", err)
	}
}

// runMigrateConfig implements -migrate-config: back up the config file, bring
// it up to the current defaults, write it back, and report what moved.
//
// Only defaults are touched. A binding the user actually chose is left alone
// even when it now collides with something else, because silently rewriting
// someone's deliberate configuration is worse than leaving a collision they
// can see in the help overlay.
func runMigrateConfig(cfg *config.Config) {
	path := config.ConfigPath()
	if _, err := os.Stat(path); err != nil {
		// Not fatal, including when TELETUI_CONFIG points somewhere that
		// does not exist: there is simply nothing to migrate, and the app
		// writes a current-default config on first run.
		fmt.Printf("No config file at %s — nothing to migrate.\n", path)
		fmt.Println("It will be written with current defaults on first run.")
		return
	}

	// Parsed a second time without defaults, so the summary can tell a key
	// the file never had from one it set to today's default, and so the
	// unexpanded "~/..." paths survive the rewrite.
	raw, err := config.LoadRawFile(path)
	if err != nil {
		fatalf("Could not read %s: %v", path, err)
	}

	changes := config.Migrate(cfg, raw)
	if len(changes) == 0 {
		fmt.Printf("%s is already up to date.\n", path)
		return
	}
	config.SortChanges(changes)

	backup, err := config.BackupFile(path)
	if err != nil {
		fatalf("Could not back up %s: %v", path, err)
	}
	if err := config.SaveTo(path, cfg); err != nil {
		fatalf("Could not write %s: %v", path, err)
	}

	fmt.Printf("Migrated %s (%d change(s)):\n\n", path, len(changes))
	for _, c := range changes {
		fmt.Printf("  %s\n", c)
	}

	// The rewrite emits the whole schema, not a patch, so it can add
	// tables the file never had and it silently drops anything the current
	// version does not recognize. Both are surprising if unannounced.
	if added := raw.MissingSections(); len(added) > 0 {
		fmt.Printf("\nAdded config section(s) with default values: [%s]\n",
			strings.Join(added, "] ["))
	}
	if dropped := raw.Unknown(); len(dropped) > 0 {
		fmt.Println("\nDropped unrecognized key(s) — this version has no such setting:")
		for _, k := range dropped {
			fmt.Printf("  %s\n", k)
		}
		fmt.Println("They are preserved in the backup if you need them back.")
	}
	if clashes := config.DetectKeyCollisions(cfg); len(clashes) > 0 {
		fmt.Println("\nWarning: some bindings now overlap. Your own choices were")
		fmt.Println("kept as-is, so a newly added default may collide with them:")
		for _, c := range clashes {
			fmt.Printf("  %s\n", c)
		}
		fmt.Println("Only the first match in the dispatch order wins; edit [keys] to resolve.")
	}

	fmt.Printf("\nBacked up to %s\n", backup)
	fmt.Println("Note: the config is re-encoded from scratch, so comments and")
	fmt.Println("key ordering in the original are lost. The backup keeps them.")
}

// fatalf reports a fatal startup or shutdown error and exits. It writes to
// stderr directly because the log package's output is discarded (see main),
// which would otherwise turn every failure into a silent exit(1).
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func setupWizard(cfg *config.Config) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("  ╔══════════════════════════════════════════╗")
	fmt.Println("  ║         Telegram CLI - First Run         ║")
	fmt.Println("  ╚══════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("  Get your API credentials from:")
	fmt.Println("  https://my.telegram.org/apps")
	fmt.Println()

	for {
		fmt.Print("  Enter API ID: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		id, err := strconv.Atoi(input)
		if err != nil || id <= 0 {
			fmt.Println("  Invalid API ID. Must be a number.")
			continue
		}
		cfg.Telegram.APIID = int32(id)
		break
	}

	for {
		fmt.Print("  Enter API Hash: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if len(input) < 10 {
			fmt.Println("  Invalid API Hash. Too short.")
			continue
		}
		cfg.Telegram.APIHash = input
		break
	}

	fmt.Print("  Enter phone number (optional, press Enter to skip): ")
	phone, _ := reader.ReadString('\n')
	phone = strings.TrimSpace(phone)
	if phone != "" {
		cfg.Telegram.Phone = phone
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println()
	fmt.Println("  Config saved! Starting Telegram CLI...")
	fmt.Println()

	return nil
}
