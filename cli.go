package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/changeset"
	"github.com/YunMuZhe/MidDeck/internal/config"
	"golang.org/x/term"
)

const changesetUsage = `Usage:
  MidDeck changeset plan   -f FILE (-i INSTANCE | --kafka ADDRS | --rabbitmq URL) [--env ENV] [--json] [--detailed-exitcode]
  MidDeck changeset apply  -f FILE (-i INSTANCE | --kafka ADDRS | --rabbitmq URL) [--env ENV] [--auto-approve] [--allow-delete] [--skip-conflicts]
  MidDeck changeset export (-i INSTANCE | --kafka ADDRS | --rabbitmq URL) [-o FILE]

-i picks a connection saved in MidDeck (id, name or alias), including its SSH jump hosts.
In CI, describe the target with flags instead; secrets come from the environment:
  MIDDECK_USERNAME, MIDDECK_PASSWORD   Kafka SASL or RabbitMQ management credentials

Exit codes: 0 success (plan: no changes with --detailed-exitcode), 1 error, 2 plan has changes (--detailed-exitcode).
`

// runChangeset implements `MidDeck changeset …`.
func runChangeset(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, changesetUsage)
		return 0
	}
	cmd := args[0]
	fs := flag.NewFlagSet("changeset "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, changesetUsage) }
	var (
		file       = fs.String("f", "", "changeset YAML file (- for stdin)")
		out        = fs.String("o", "", "output file for export (default stdout)")
		ref        = fs.String("i", "", "saved MidDeck connection: id, name or alias")
		env        = fs.String("env", "", "environment for envs.<env> overrides (default: the connection's)")
		kafkaAddrs = fs.String("kafka", "", "Kafka bootstrap servers, comma separated")
		sasl       = fs.String("sasl", "", "Kafka SASL mechanism: plain | scram-sha-256 | scram-sha-512")
		rabbitURL  = fs.String("rabbitmq", "", "RabbitMQ management URL, e.g. http://host:15672")
		vhost      = fs.String("vhost", "/", "RabbitMQ vhost")
		useTLS     = fs.Bool("tls", false, "use TLS")
		insecure   = fs.Bool("tls-insecure", false, "skip TLS certificate verification")
		caFile     = fs.String("ca-file", "", "CA certificate (PEM)")
		asJSON     = fs.Bool("json", false, "print the plan as JSON")
		detailed   = fs.Bool("detailed-exitcode", false, "plan: exit 2 when there are changes")
		approve    = fs.Bool("auto-approve", false, "apply without asking")
		allowDel   = fs.Bool("allow-delete", false, "allow state: absent to delete resources")
		skipConf   = fs.Bool("skip-conflicts", false, "apply the rest when some changes conflict")
	)
	fs.StringVar(ref, "instance", "", "same as -i")
	fs.StringVar(file, "file", "", "same as -f")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	switch cmd {
	case "plan", "apply", "export":
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", cmd, changesetUsage)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	a := NewApp()
	if err := a.init(ctx); err != nil {
		return fail(err)
	}
	defer a.shutdown(ctx)

	// Target.
	var id, label string
	switch n := btoi(*ref != "") + btoi(*kafkaAddrs != "") + btoi(*rabbitURL != ""); {
	case n == 0:
		return fail(errors.New("choose a target with -i, --kafka or --rabbitmq"))
	case n > 1:
		return fail(errors.New("-i, --kafka and --rabbitmq are mutually exclusive"))
	}
	if *ref != "" {
		inst, err := a.resolve(*ref)
		if err != nil {
			return fail(err)
		}
		id, label = inst.ID, inst.Name
		if inst.Env != "" {
			label += " (" + inst.Env + ")"
		}
		if *env == "" {
			*env = inst.Env
		}
	} else {
		inst := config.Instance{Name: "cli", Env: *env,
			Auth: config.Auth{Username: os.Getenv("MIDDECK_USERNAME"), Password: os.Getenv("MIDDECK_PASSWORD")},
			TLS:  config.TLS{Enabled: *useTLS || *caFile != "", InsecureSkipVerify: *insecure, CAFile: *caFile}}
		if *kafkaAddrs != "" {
			inst.Kind, inst.Addrs, inst.Auth.Mechanism = config.KindKafka, splitList(*kafkaAddrs), *sasl
			label = *kafkaAddrs
		} else {
			inst.Kind, inst.MgmtURL, inst.VHost = config.KindRabbitMQ, *rabbitURL, *vhost
			label = *rabbitURL
		}
		inst.Name = label
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		var err error
		id, err = a.connectInline(cctx, inst)
		cancel()
		if err != nil {
			return fail(err)
		}
	}

	if cmd == "export" {
		c, err := a.get(id)
		if err != nil {
			return fail(err)
		}
		y, err := exportChangeset(ctx, c)
		if err != nil {
			return fail(err)
		}
		if *out == "" || *out == "-" {
			fmt.Fprint(stdout, y)
			return 0
		}
		if err := os.WriteFile(*out, []byte(y), 0o644); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stderr, "Exported %s to %s\n", label, *out)
		return 0
	}

	if *file == "" {
		return fail(errors.New("-f FILE is required"))
	}
	var text []byte
	var err error
	if *file == "-" {
		text, err = io.ReadAll(os.Stdin)
	} else {
		text, err = os.ReadFile(*file)
	}
	if err != nil {
		return fail(err)
	}
	p, c, err := a.planFor(ctx, id, string(text), *env)
	if err != nil {
		return fail(err)
	}
	if *asJSON && cmd == "plan" {
		b, _ := json.MarshalIndent(p, "", "  ")
		fmt.Fprintln(stdout, string(b))
	} else {
		fmt.Fprintf(stdout, "Target: %s · env %s\n\n", label, orDash(p.Env))
		fmt.Fprint(stdout, changeset.Format(p))
	}
	if cmd == "plan" {
		if *detailed && len(p.Changes) > 0 {
			return 2
		}
		return 0
	}

	// apply
	if len(p.Changes) == 0 {
		fmt.Fprintln(stdout, "\nNothing to do.")
		return 0
	}
	if !*approve {
		if !term.IsTerminal(int(os.Stdin.Fd())) || *file == "-" {
			return fail(errors.New("not a terminal: pass --auto-approve to apply without a prompt"))
		}
		want := "yes"
		if c.inst.IsProd() {
			want = c.inst.Name
		}
		fmt.Fprintf(stdout, "\nType %q to apply: ", want)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != want {
			fmt.Fprintln(stdout, "Cancelled.")
			return 1
		}
	}
	res, err := a.applyChangeset(ctx, id, string(text), *env, changeset.ApplyOptions{
		Fingerprint: p.Fingerprint, AllowDelete: *allowDel, SkipConflicts: *skipConf,
	})
	if errors.Is(err, changeset.ErrConflicts) {
		err = errors.New("the plan has conflicts that cannot be applied in place; fix them or pass --skip-conflicts")
	}
	if err != nil {
		return fail(err)
	}
	fmt.Fprintln(stdout)
	code := 0
	for _, r := range res {
		mark := map[string]string{"applied": "ok  ", "failed": "FAIL", "skipped": "skip"}[r.Status]
		fmt.Fprintf(stdout, "  %s %s %s %s", mark, r.Action, r.Resource, r.Name)
		if r.Error != "" {
			fmt.Fprintf(stdout, ": %s", r.Error)
			code = 1
		}
		fmt.Fprintln(stdout)
	}
	if code == 0 {
		fmt.Fprintln(stdout, "\nApply complete.")
	}
	return code
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func splitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
