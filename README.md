# pingstack

Wrap any scheduled job and report it to a [Pingstack](https://pingstack.io)
heartbeat check. When the job fails you're alerted right away, with its exit
code and the end of its output; when it doesn't run at all, you're alerted
once it's overdue. Pingstack also records how long each run took.

```bash
pingstack run -- /usr/local/bin/backup.sh
```

`pingstack run` passes the job's output through unchanged and exits with the
job's own exit code, so cron, systemd and CI see exactly what they did before.

## Install

**macOS (Homebrew)**

```bash
brew install pingstack-io/tap/pingstack
```

**macOS and Linux (install script)**

```bash
curl -fsSL https://github.com/pingstack-io/pingstack-ping/releases/latest/download/install.sh | sh
```

The script picks the right build for your machine, checks it against the
release's checksums, and installs to `/usr/local/bin` (or `~/.local/bin`).

**Windows, or any platform by hand:** download the archive for your system
from the [latest release](https://github.com/pingstack-io/pingstack-ping/releases/latest)
and put `pingstack` (`pingstack.exe`) on your `PATH`.

**With Go:** `go install github.com/pingstack-io/pingstack-ping/cmd/pingstack@latest`

## Use it

1. Create a heartbeat check in Pingstack with the same schedule as your job.
2. Give the job the check's ping URL as `PINGSTACK_PING_URL`. Keep it private:
   anyone who has it can report to your check.
3. Put `pingstack run --` in front of the job's command.

A crontab entry:

```
PINGSTACK_PING_URL=https://pingstack.io/ping/YOUR_TOKEN
0 2 * * * pingstack run -- /usr/local/bin/backup.sh
```

A systemd service:

```ini
[Service]
Environment=PINGSTACK_PING_URL=https://pingstack.io/ping/YOUR_TOKEN
ExecStart=/usr/local/bin/pingstack run -- /usr/local/bin/backup.sh
```

## What it sends

| When | Request |
| --- | --- |
| The job starts | the ping URL's `/start`, so the run time can be recorded |
| It exits 0 | the ping URL with `exit_code=0` |
| It exits non-zero, or can't start | the ping URL's `/fail` with the exit code and the last 100 lines of output |

On success no output is sent, so routine logs never leave the machine. A
report that can't be delivered prints a warning, after retries, and never
changes the exit code: monitoring never breaks your job. If the ping URL is
missing or isn't a Pingstack address, the job still runs and nothing is sent.

## Commands

```
pingstack run [flags] -- <command> [args...]
pingstack ping [flags]
pingstack version
```

`run` flags:

| Flag | Default | Description |
| --- | --- | --- |
| `--url` | `$PINGSTACK_PING_URL` | The check's ping URL. Prefer the environment variable, which stays out of `ps` and shell history. |
| `--value` | | A number checked against the check's warning and critical thresholds. |
| `--value-from-output` | off | Send the last number the job printed as the value. |
| `--tail` | `100` | Lines of output sent with a failure (at most 10 KB). `0` sends none. |
| `--no-start` | off | Don't report the start, so no run time is recorded. |
| `--retries` | `5` | Retries after a network error, a 429 or a 5xx, waiting 1, 2, 4… seconds. |
| `--timeout` | `10s` | Time to wait for each request. |
| `--quiet` | off | Print nothing of pingstack's own. The job's output still passes through. |

`ping` sends one report, for scripts that report on their own. It takes
`--url`, `--value`, `--retries`, `--timeout` and `--quiet`, plus:

| Flag | Description |
| --- | --- |
| `--fail` | Report a failure. |
| `--start` | Report that the job is starting. |
| `--exit-code` | Record an exit code with the report. |
| `--message` | Text stored with the report. |

`ping` exits 1 when the report couldn't be delivered and 2 for a usage error.

## Signals

Ctrl-C, `SIGTERM`, `SIGHUP` and `SIGQUIT` are passed to the job. pingstack
waits for it to exit, reports a job killed by a signal as a failure (exit code
128 + the signal, as shells do), and exits with that code.

## Security

The ping URL is only sent to `https://` addresses on `pingstack.io`, and it's
never printed, including in error messages. pingstack is a single binary with
no dependencies beyond Go's standard library, built in this repository's
release workflow with checksums for every download.

## License

MIT
