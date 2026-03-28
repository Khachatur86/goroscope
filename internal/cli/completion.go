package cli

import (
	"fmt"
	"io"
)

// completionSubcommand dispatches to the shell-specific generator.
func completionSubcommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "Usage: goroscope completion <shell>")
		_, _ = fmt.Fprintln(stderr, "Shells: zsh, bash, fish")
		return fmt.Errorf("missing shell argument")
	}
	switch args[0] {
	case "zsh":
		_, _ = fmt.Fprint(stdout, zshCompletion)
	case "bash":
		_, _ = fmt.Fprint(stdout, bashCompletion)
	case "fish":
		_, _ = fmt.Fprint(stdout, fishCompletion)
	default:
		return fmt.Errorf("unknown shell %q; supported: zsh, bash, fish", args[0])
	}
	return nil
}

// ── Zsh ───────────────────────────────────────────────────────────────────────

const zshCompletion = `#compdef goroscope

_goroscope() {
  local state

  _arguments \
    '1: :->cmd' \
    '*: :->args'

  case $state in
    cmd)
      _values 'command' \
        'attach[Attach to a running Go process via pprof]' \
        'run[Run a Go program with live trace capture]' \
        'test[Run go test with tracing and open UI]' \
        'ui[Load demo data and serve UI]' \
        'collect[Load demo data and serve UI]' \
        'replay[Load a .gtrace file and serve UI]' \
        'check[Analyze capture for deadlock hints]' \
        'export[Export timeline segments to CSV/JSON/OTLP]' \
        'watch[Live anomaly alerting]' \
        'top[Live goroutine table (htop-style)]' \
        'doctor[Generate HTML diagnostic report from .gtrace]' \
        'diff[Compare two .gtrace files]' \
        'completion[Generate shell completion script]' \
        'annotate[Add/list/delete annotations in a .gtrace file]' \
        'history[List saved captures]' \
        'version[Print version]' \
        'help[Show help]'
      ;;
    args)
      case $words[2] in
        attach)
          _arguments \
            '--addr=[HTTP bind address]:addr:(127.0.0.1:7070)' \
            '--open-browser[Open browser when ready]' \
            '--interval=[Poll interval]:duration:(2s 5s 10s)' \
            '--session-name=[Session name]:name' \
            '--max-goroutines=[Max goroutines to display]:n' \
            '--max-segments=[Max timeline segments]:n' \
            '--max-stacks=[Max stacks per goroutine]:n' \
            '--flight-recorder[Use flight recorder endpoint]' \
            '--tls-cert=[TLS certificate PEM file]:file:_files' \
            '--tls-key=[TLS private key PEM file]:file:_files' \
            '--token=[Bearer token]:token' \
            '*:url:_urls'
          ;;
        run)
          _arguments \
            '--addr=[HTTP bind address]:addr' \
            '--open-browser[Open browser when ready]' \
            '--save=[Save capture path]:file:_files' \
            '--max-goroutines=[Max goroutines]:n' \
            '--tls-cert=[TLS cert]:file:_files' \
            '--tls-key=[TLS key]:file:_files' \
            '--token=[Bearer token]:token' \
            '*:package:_go_packages'
          ;;
        test)
          _arguments \
            '--addr=[HTTP bind address]:addr' \
            '--open-browser[Open browser when ready]' \
            '--filter=[Pre-populate UI search]:term' \
            '--save=[Save capture path]:file:_files' \
            '*:package:_go_packages'
          ;;
        ui)
          _arguments \
            '--addr=[HTTP bind address]:addr' \
            '--open-browser[Open browser when ready]' \
            '--target=[Monitor live process (repeatable)]:url:_urls' \
            '--max-goroutines=[Max goroutines]:n' \
            '--tls-cert=[TLS cert]:file:_files' \
            '--tls-key=[TLS key]:file:_files' \
            '--token=[Bearer token]:token'
          ;;
        replay)
          _arguments \
            '--addr=[HTTP bind address]:addr' \
            '--open-browser[Open browser when ready]' \
            '--max-goroutines=[Max goroutines]:n' \
            '--tls-cert=[TLS cert]:file:_files' \
            '--tls-key=[TLS key]:file:_files' \
            '--token=[Bearer token]:token' \
            '*:file:_files -g "*.gtrace *.trace"'
          ;;
        check)
          _arguments \
            '--format=[Output format]:fmt:(text json github dot sarif)' \
            '*:file:_files -g "*.gtrace"'
          ;;
        export)
          _arguments \
            '--format=[Output format]:fmt:(csv json otlp)' \
            '--endpoint=[OTLP endpoint]:endpoint' \
            '*:file:_files -g "*.gtrace"'
          ;;
        watch)
          _arguments \
            '--addr=[HTTP bind address]:addr' \
            '--interval=[Check interval]:duration:(5s 10s 30s)' \
            '--threshold=[Alert threshold]:n' \
            '*:url:_urls'
          ;;
        top)
          _arguments \
            '--interval=[Refresh interval]:duration:(1s 2s 5s)' \
            '--n=[Rows to display]:n' \
            '--once[Print one frame and exit]' \
            '*:url:_urls'
          ;;
        doctor)
          _arguments \
            '*:file:_files -g "*.gtrace"'
          ;;
        diff)
          _arguments \
            '--format=[Output format]:fmt:(text json)' \
            '--threshold=[Regression threshold]:pct:(5% 10% 20%)' \
            '*:file:_files -g "*.gtrace"'
          ;;
        completion)
          _values 'shell' zsh bash fish
          ;;
        annotate)
          _arguments \
            '--list[List annotations]' \
            '--at=[Timestamp or duration]:time' \
            '--note=[Annotation text]:note' \
            '--delete=[Annotation ID to delete]:id' \
            '*:file:_files -g "*.gtrace"'
          ;;
      esac
      ;;
  esac
}

_goroscope
`

// ── Bash ──────────────────────────────────────────────────────────────────────

const bashCompletion = `# bash completion for goroscope
_goroscope_completions() {
  local cur prev words cword
  _init_completion 2>/dev/null || {
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
  }

  local commands="attach run test ui collect replay check export watch top doctor diff completion annotate history version help"

  if [[ $COMP_CWORD -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "$commands" -- "$cur") )
    return
  fi

  local cmd="${COMP_WORDS[1]}"

  # Flag value completions
  case "$prev" in
    --format)
      case "$cmd" in
        check)  COMPREPLY=( $(compgen -W "text json github dot sarif" -- "$cur") ); return ;;
        export) COMPREPLY=( $(compgen -W "csv json otlp" -- "$cur") ); return ;;
        diff)   COMPREPLY=( $(compgen -W "text json" -- "$cur") ); return ;;
      esac
      ;;
    --addr|--endpoint|--token|--session-name|--filter|--threshold) return ;;
    --interval)
      COMPREPLY=( $(compgen -W "1s 2s 5s 10s 30s" -- "$cur") ); return ;;
    --tls-cert|--tls-key|--save)
      COMPREPLY=( $(compgen -f -- "$cur") ); return ;;
  esac

  # Per-command flags
  local flags=""
  case "$cmd" in
    attach)
      flags="--addr --open-browser --interval --session-name --max-goroutines
             --max-segments --max-stacks --flight-recorder --tls-cert --tls-key --token"
      ;;
    run)
      flags="--addr --open-browser --save --max-goroutines --tls-cert --tls-key --token"
      ;;
    test)
      flags="--addr --open-browser --filter --save"
      ;;
    ui)
      flags="--addr --open-browser --target --max-goroutines --tls-cert --tls-key --token"
      ;;
    replay)
      flags="--addr --open-browser --max-goroutines --tls-cert --tls-key --token"
      COMPREPLY=( $(compgen -W "$flags" -- "$cur") $(compgen -f -X '!*.gtrace' -- "$cur") )
      return
      ;;
    check)
      flags="--format"
      COMPREPLY=( $(compgen -W "$flags" -- "$cur") $(compgen -f -X '!*.gtrace' -- "$cur") )
      return
      ;;
    export)
      flags="--format --endpoint"
      COMPREPLY=( $(compgen -W "$flags" -- "$cur") $(compgen -f -X '!*.gtrace' -- "$cur") )
      return
      ;;
    watch)
      flags="--addr --interval --threshold"
      ;;
    top)
      flags="--interval --n --once"
      ;;
    doctor)
      COMPREPLY=( $(compgen -f -X '!*.gtrace' -- "$cur") )
      return
      ;;
    diff)
      flags="--format --threshold"
      COMPREPLY=( $(compgen -W "$flags" -- "$cur") $(compgen -f -X '!*.gtrace' -- "$cur") )
      return
      ;;
    completion)
      COMPREPLY=( $(compgen -W "zsh bash fish" -- "$cur") ); return ;;
    annotate)
      flags="--list --at --note --delete"
      COMPREPLY=( $(compgen -W "$flags" -- "$cur") $(compgen -f -X '!*.gtrace' -- "$cur") )
      return ;;
  esac

  if [[ "$cur" == -* ]]; then
    COMPREPLY=( $(compgen -W "$flags" -- "$cur") )
  fi
}

complete -F _goroscope_completions goroscope
`

// ── Fish ──────────────────────────────────────────────────────────────────────

const fishCompletion = `# fish completion for goroscope
set -l commands attach run test ui collect replay check export watch top doctor diff completion annotate history version help

# Disable file completions for the top-level command
complete -c goroscope -f

# Top-level commands
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a attach    -d "Attach to a running Go process via pprof"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a run       -d "Run a Go program with live trace capture"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a test      -d "Run go test with tracing and open UI"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a ui        -d "Load demo data and serve UI"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a replay    -d "Load a .gtrace file and serve UI"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a check     -d "Analyze capture for deadlock hints"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a export    -d "Export timeline segments"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a watch     -d "Live anomaly alerting"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a top       -d "Live goroutine table (htop-style)"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a doctor    -d "Generate HTML diagnostic report from .gtrace"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a diff      -d "Compare two .gtrace files"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a completion -d "Generate shell completion script"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a annotate  -d "Add/list/delete annotations in a .gtrace file"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a history   -d "List saved captures"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a version   -d "Print version"
complete -c goroscope -n "not __fish_seen_subcommand_from $commands" \
  -a help      -d "Show help"

# completion subcommand shells
complete -c goroscope -n "__fish_seen_subcommand_from completion" -a "zsh bash fish" -f

# Shared flags
for cmd in attach run test ui replay check export watch diff
  complete -c goroscope -n "__fish_seen_subcommand_from $cmd" \
    -l addr -d "HTTP bind address" -x
  complete -c goroscope -n "__fish_seen_subcommand_from $cmd" \
    -l open-browser -d "Open browser when ready"
end

# attach-specific
complete -c goroscope -n "__fish_seen_subcommand_from attach" \
  -l interval -d "Poll interval" -x -a "1s 2s 5s 10s"
complete -c goroscope -n "__fish_seen_subcommand_from attach" \
  -l session-name -d "Session name" -x
complete -c goroscope -n "__fish_seen_subcommand_from attach" \
  -l max-goroutines -d "Max goroutines" -x
complete -c goroscope -n "__fish_seen_subcommand_from attach" \
  -l flight-recorder -d "Use flight recorder endpoint"
complete -c goroscope -n "__fish_seen_subcommand_from attach" \
  -l token -d "Bearer token" -x

# test-specific
complete -c goroscope -n "__fish_seen_subcommand_from test" \
  -l filter -d "Pre-populate UI search" -x
complete -c goroscope -n "__fish_seen_subcommand_from test" \
  -l save -d "Save capture path" -r

# ui-specific
complete -c goroscope -n "__fish_seen_subcommand_from ui" \
  -l target -d "Monitor live process (repeatable)" -x

# check-specific
complete -c goroscope -n "__fish_seen_subcommand_from check" \
  -l format -d "Output format" -x -a "text json github dot sarif"

# export-specific
complete -c goroscope -n "__fish_seen_subcommand_from export" \
  -l format -d "Output format" -x -a "csv json otlp"
complete -c goroscope -n "__fish_seen_subcommand_from export" \
  -l endpoint -d "OTLP endpoint" -x

# diff-specific
complete -c goroscope -n "__fish_seen_subcommand_from diff" \
  -l format -d "Output format" -x -a "text json"
complete -c goroscope -n "__fish_seen_subcommand_from diff" \
  -l threshold -d "Regression threshold" -x -a "5% 10% 20%"

# annotate-specific
complete -c goroscope -n "__fish_seen_subcommand_from annotate" \
  -l list -d "List annotations"
complete -c goroscope -n "__fish_seen_subcommand_from annotate" \
  -l at -d "Timestamp or duration" -x
complete -c goroscope -n "__fish_seen_subcommand_from annotate" \
  -l note -d "Annotation text" -x
complete -c goroscope -n "__fish_seen_subcommand_from annotate" \
  -l delete -d "Annotation ID to delete" -x

# File completions for commands that accept .gtrace files
for cmd in replay check export diff annotate
  complete -c goroscope -n "__fish_seen_subcommand_from $cmd" -a "(ls *.gtrace 2>/dev/null)" -f
end
`
