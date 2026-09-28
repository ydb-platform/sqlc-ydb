package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/config"
)

const completionHelp = `Print a completion script for the selected shell.

Usage:
  sqlc-ydb completion bash|zsh|fish|powershell

See docs/installation.md for installation instructions.
`

const completionCommands = "generate compile verify vet diff init version completion help"

func printCompletion(shell string, w io.Writer) error {
	languages := map[string]bool{}
	runtimes := map[string]bool{}
	for _, generator := range config.Generators() {
		languages[generator.Language] = true
		for _, runtime := range generator.Runtimes {
			runtimes[runtime] = true
		}
		for alias := range generator.RuntimeAliases {
			runtimes[alias] = true
		}
	}
	values := func(set map[string]bool) string {
		var names []string
		for name := range set {
			names = append(names, name)
		}
		sort.Strings(names)
		return strings.Join(names, " ")
	}
	var script string
	switch shell {
	case "bash":
		script = fmt.Sprintf(bashCompletion, values(languages), values(runtimes), values(languages), values(runtimes), completionCommands)
	case "zsh":
		script = fmt.Sprintf(zshCompletion, completionCommands, values(languages), values(runtimes), values(languages), values(runtimes))
	case "fish":
		script = fmt.Sprintf(fishCompletion, completionCommands, values(languages), values(runtimes))
	case "powershell":
		script = fmt.Sprintf(powershellCompletion, completionCommands, values(languages), values(runtimes))
	default:
		return fmt.Errorf("unsupported completion shell %q; choose bash, zsh, fish, or powershell", shell)
	}
	_, err := io.WriteString(w, script)
	return err
}

const bashCompletion = `_sqlc_ydb_complete() {
  local cur prev cmd word opts path prefix value
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev=""
  if (( COMP_CWORD > 0 )); then prev="${COMP_WORDS[COMP_CWORD-1]}"; fi
  case "$prev" in
    -f|--file|--against)
      compopt -o filenames
      while IFS= read -r path; do COMPREPLY+=("$path"); done < <(compgen -f -- "$cur")
      return ;;
    --language) COMPREPLY=( $(compgen -W '%s' -- "$cur") ); return ;;
    --runtime) COMPREPLY=( $(compgen -W '%s' -- "$cur") ); return ;;
  esac
  case "$cur" in
    --file=*|--against=*)
      prefix="${cur%%%%=*}="
      value="${cur#*=}"
      compopt -o filenames
      while IFS= read -r path; do COMPREPLY+=("$prefix$path"); done < <(compgen -f -- "$value")
      return ;;
    --language=*|--runtime=*)
      prefix="${cur%%%%=*}="
      value="${cur#*=}"
      if [[ "$prefix" == '--language=' ]]; then opts='%s'; else opts='%s'; fi
      for word in $(compgen -W "$opts" -- "$value"); do COMPREPLY+=("$prefix$word"); done
      return ;;
  esac
  cmd=""
  for word in "${COMP_WORDS[@]:1:COMP_CWORD-1}"; do
    case "$word" in
      generate|compile|verify|vet|diff|init|version|completion|help) cmd="$word"; break ;;
    esac
  done
  if [[ -z "$cmd" && "$cur" != -* ]]; then
    COMPREPLY=( $(compgen -W '%s' -- "$cur") )
    return
  fi
  if [[ "$cmd" == completion && "$cur" != -* ]]; then
    COMPREPLY=( $(compgen -W 'bash zsh fish powershell' -- "$cur") )
    return
  fi
  opts='-h --help -f --file'
  case "$cmd" in
    generate|compile|diff|vet) opts="$opts --no-database" ;;
    verify) opts="$opts --against --no-database" ;;
    init) opts="$opts --language --runtime --all-options --v2" ;;
    version) opts='-h --help --verbose --no-remote --upgrade' ;;
    completion|help) opts='-h --help' ;;
  esac
  COMPREPLY=( $(compgen -W "$opts" -- "$cur") )
}
complete -o bashdefault -o default -F _sqlc_ydb_complete sqlc-ydb
`

const zshCompletion = `#compdef sqlc-ydb
_sqlc_ydb() {
  local cmd word
  local -a commands options
  commands=(%s)
  cmd=''
  for word in "${words[@]}"; do
    case "$word" in
      generate|compile|verify|vet|diff|init|version|completion|help) cmd="$word"; break ;;
    esac
  done
  case "${words[CURRENT-1]}" in
    -f|--file|--against) _files; return ;;
    --language) _values 'language' %s; return ;;
    --runtime) _values 'runtime' %s; return ;;
  esac
  case "${words[CURRENT]}" in
    --file=*|--against=*) compset -P '*='; _files; return ;;
    --language=*) compset -P '*='; _values 'language' %s; return ;;
    --runtime=*) compset -P '*='; _values 'runtime' %s; return ;;
  esac
  if [[ -z "$cmd" && "${words[CURRENT]}" != -* ]]; then
    _values 'command' "${commands[@]}"
    return
  fi
  if [[ "$cmd" == completion && "${words[CURRENT]}" != -* ]]; then
    _values 'shell' bash zsh fish powershell
    return
  fi
  options=(-h --help -f --file)
  case "$cmd" in
    generate|compile|diff|vet) options+=(--no-database) ;;
    verify) options+=(--against --no-database) ;;
    init) options+=(--language --runtime --all-options --v2) ;;
    version) options=(-h --help --verbose --no-remote --upgrade) ;;
    completion|help) options=(-h --help) ;;
  esac
  _values 'option' "${options[@]}"
}
compdef _sqlc_ydb sqlc-ydb
`

const fishCompletion = `function __fish_sqlc_ydb_needs_command
    for word in (commandline -opc)
        switch $word
            case generate compile verify vet diff init version completion help
                return 1
        end
    end
    return 0
end
function __fish_sqlc_ydb_using_command --argument-names wanted
    contains -- $wanted (commandline -opc)
end
complete -c sqlc-ydb -n '__fish_sqlc_ydb_needs_command' -f -a '%s'
complete -c sqlc-ydb -l help -s h
complete -c sqlc-ydb -n '__fish_sqlc_ydb_needs_command; or __fish_sqlc_ydb_using_command generate; or __fish_sqlc_ydb_using_command compile; or __fish_sqlc_ydb_using_command diff; or __fish_sqlc_ydb_using_command verify; or __fish_sqlc_ydb_using_command vet; or __fish_sqlc_ydb_using_command init' -l file -s f -r -F
complete -c sqlc-ydb -n '__fish_sqlc_ydb_using_command verify' -l against -r -F
complete -c sqlc-ydb -n '__fish_sqlc_ydb_using_command generate; or __fish_sqlc_ydb_using_command compile; or __fish_sqlc_ydb_using_command diff; or __fish_sqlc_ydb_using_command verify; or __fish_sqlc_ydb_using_command vet' -l no-database
complete -c sqlc-ydb -n '__fish_sqlc_ydb_using_command init' -l language -r -a '%s'
complete -c sqlc-ydb -n '__fish_sqlc_ydb_using_command init' -l runtime -r -a '%s'
complete -c sqlc-ydb -n '__fish_sqlc_ydb_using_command init' -l all-options
complete -c sqlc-ydb -n '__fish_sqlc_ydb_using_command init' -l v2
complete -c sqlc-ydb -n '__fish_sqlc_ydb_using_command version' -l verbose
complete -c sqlc-ydb -n '__fish_sqlc_ydb_using_command version' -l no-remote
complete -c sqlc-ydb -n '__fish_sqlc_ydb_using_command version' -l upgrade
complete -c sqlc-ydb -n '__fish_sqlc_ydb_using_command completion' -f -a 'bash zsh fish powershell'
`

const powershellCompletion = `Register-ArgumentCompleter -Native -CommandName sqlc-ydb -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $commands = '%s'.Split(' ')
    $tokens = @($commandAst.CommandElements | Select-Object -Skip 1 | ForEach-Object { $_.Extent.Text })
    $command = $tokens | Where-Object { $commands -contains $_ } | Select-Object -First 1
    $previous = if ($tokens.Count -gt 1 -and $tokens[-1] -eq $wordToComplete) { $tokens[-2] }
        elseif ($tokens.Count -gt 0) { $tokens[-1] } else { '' }
    $pathWord = $wordToComplete
    $prefix = ''
    if ($wordToComplete -match '^(--file|--against)=(.*)$') {
        $prefix = $Matches[1] + '='
        $pathWord = $Matches[2]
    }
    if ($previous -in @('-f', '--file', '--against') -or $prefix) {
        $directory = '.'
        $leaf = $pathWord
        if ($pathWord -match '[/\\]') {
            $directory = Split-Path $pathWord -Parent
            $leaf = Split-Path $pathWord -Leaf
        }
        Get-ChildItem -LiteralPath $directory -ErrorAction SilentlyContinue | Where-Object {
            $_.Name.StartsWith($leaf, [System.StringComparison]::OrdinalIgnoreCase)
        } | ForEach-Object {
            $path = if ($directory -eq '.') { $_.Name } else { Join-Path $directory $_.Name }
            if ($path -match '\s') { $path = "'" + $path.Replace("'", "''") + "'" }
            [System.Management.Automation.CompletionResult]::new($prefix + $path, $_.Name, 'ProviderItem', $_.Name)
        }
        return
    }
    $valuePrefix = ''
    $valueWord = $wordToComplete
    if ($wordToComplete -match '^(--language|--runtime)=(.*)$') {
        $valuePrefix = $Matches[1] + '='
        $valueWord = $Matches[2]
    }
    $choices = if ($previous -eq '--language' -or $valuePrefix -eq '--language=') { '%s'.Split(' ') }
        elseif ($previous -eq '--runtime' -or $valuePrefix -eq '--runtime=') { '%s'.Split(' ') }
        elseif ($command -eq 'completion' -and -not $wordToComplete.StartsWith('-')) { @('bash', 'zsh', 'fish', 'powershell') }
        elseif (-not $command -and -not $wordToComplete.StartsWith('-')) { $commands }
        else {
            $options = @('-h', '--help', '-f', '--file')
            switch ($command) {
                { $_ -in @('generate', 'compile', 'diff', 'vet') } { $options += '--no-database' }
                'verify' { $options += @('--against', '--no-database') }
                'init' { $options += @('--language', '--runtime', '--all-options', '--v2') }
                'version' { $options = @('-h', '--help', '--verbose', '--no-remote', '--upgrade') }
                { $_ -in @('completion', 'help') } { $options = @('-h', '--help') }
            }
            $options
        }
    foreach ($choice in $choices) {
        if ($choice.StartsWith($valueWord, [System.StringComparison]::OrdinalIgnoreCase)) {
            [System.Management.Automation.CompletionResult]::new($valuePrefix + $choice, $choice, 'ParameterValue', $choice)
        }
    }
}
`
