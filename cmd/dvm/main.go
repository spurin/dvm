// Command dvm is a cross-platform launcher that boots a QEMU VM from components
// pulled as OCI artifacts and forwards guest ports to the host.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/spurin/diveinto-lab-cli/internal/app"
)

const usage = `dvm - boot a VM from OCI components and forward guest ports to localhost

Usage:
  dvm [command] [flags]
  dvm start [flags]                  (start is the default if no command is given)

Commands:
  start     Boot the VM and supervise it in the foreground (default)
  console   Boot the VM with the terminal attached to the guest serial console
  status    Show whether the VM is running and its exposed ports
  clean     Remove the extracted asset cache (and optionally VM state)
  version   Print version information

Running and stopping:
  start and console run in the foreground. Press Ctrl-C to shut the guest down
  gracefully (Ctrl-] first to detach the console). Run "dvm status" from another
  terminal to check on it. A detached background mode, and a matching "dvm stop",
  are not implemented yet.

Configuration:
  Any flag can come from a YAML config file; CLI flags override it. dvm reads
  --config <path-or-url> if given, otherwise ./dvm.yaml, otherwise the user
  config dir. --config also accepts an http(s) URL, for example:
    dvm start --config https://example.com/dvm.yaml --guest-password lab

Run "dvm <command> -h" for the full flag list.
`

// stringSlice is a repeatable string flag (e.g. --port a --port b).
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	args := os.Args[1:]
	command := "start"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}

	switch command {
	case "version":
		fmt.Println(app.VersionInfo("", "", ""))
		return
	case "start", "console", "status", "clean", "stop":
		if err := runCommand(command, args); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", command, usage)
		os.Exit(2)
	}
}

func runCommand(command string, args []string) error {
	fs := flag.NewFlagSet(command, flag.ExitOnError)
	var (
		f               app.Flags
		ports           stringSlice
		qemuArgs        stringSlice
		noAccelFallback bool
		withState       bool
	)
	fs.StringVar(&f.ConfigPath, "config", "", "config file: a local path or an http(s) URL")
	fs.StringVar(&f.DataDir, "data-dir", "", "override runtime data (state) directory")
	fs.StringVar(&f.CacheDir, "cache-dir", "", "override extracted asset cache directory")
	fs.StringVar(&f.Engine, "engine", "auto", "virtualization engine: auto | qemu | vz (auto picks the native macOS hypervisor when available, else qemu)")

	fs.StringVar(&f.Kernel, "kernel", "", "kernel reference (oci ref or local path)")
	fs.StringVar(&f.Initrd, "initrd", "", "initrd reference (oci ref or local path)")
	fs.StringVar(&f.Rootfs, "rootfs", "", "rootfs reference, qcow2 or raw (oci ref or local path)")
	fs.StringVar(&f.RootfsVZ, "rootfs-vz", "", "raw/ext4 rootfs for --engine vz (defaults to --rootfs)")
	fs.StringVar(&f.QEMUDir, "qemu-dir", "", "directory containing qemu-system-* and qemu-img")

	fs.StringVar(&f.GuestArch, "guest-arch", "", "guest architecture: x86_64|arm64")
	fs.StringVar(&f.KernelCmdline, "kernel-cmdline", "", "override kernel command line")
	fs.IntVar(&f.MemoryMB, "memory", 0, "guest memory in MB")
	fs.IntVar(&f.CPUs, "cpus", 0, "guest vCPU count")

	fs.Var(&ports, "port", "host:guest port forward (repeatable), e.g. 8080:80")
	fs.StringVar(&f.IPConfig, "ip-config", "", "guest IP config: cloud-init|kernel-dhcp|kernel-static|none")
	fs.StringVar(&f.BindAddress, "bind-address", "", "host bind address for forwards (default 127.0.0.1)")
	fs.IntVar(&f.SSHPort, "ssh-port", 0, "expose guest SSH (port 22) on this host port")

	fs.BoolVar(&f.NoPersist, "no-persist", false, "run with ephemeral VM state")
	fs.BoolVar(&f.Reset, "reset", false, "delete existing writable state before starting")
	fs.BoolVar(&noAccelFallback, "no-accel-fallback", false, "do not fall back to software emulation")
	fs.BoolVar(&f.Headless, "headless", true, "run without a graphical display")
	fs.BoolVar(&f.Debug, "debug", false, "print the full QEMU command and extra diagnostics")
	fs.Var(&qemuArgs, "qemu-arg", "append a raw QEMU argument (advanced, repeatable)")

	fs.BoolVar(&f.Console, "console", false, "attach the terminal to the guest serial console")
	fs.StringVar(&f.GuestPassword, "guest-password", "", "set a console login password via cloud-init")
	fs.StringVar(&f.SSHKeyPath, "ssh-key", "", "public key file to authorize in the guest")
	fs.StringVar(&f.SeedPath, "seed", "", "use a pre-built cloud-init seed image instead of generating one")

	if command == "clean" {
		fs.BoolVar(&withState, "with-state", false, "also remove the VM state directory")
	}
	fs.Parse(args)

	f.Ports = ports
	f.QemuArgs = qemuArgs
	f.AccelFallback = !noAccelFallback
	if command == "console" {
		f.Console = true
	}

	a, err := app.New(f)
	if err != nil {
		return err
	}

	switch command {
	case "status":
		return a.Status()
	case "clean":
		return a.Clean(withState)
	case "stop":
		return fmt.Errorf("no detached/background mode yet: start runs in the foreground, so press Ctrl-C in its terminal to stop the VM")
	default: // start, console
		a.SetRuntimeFlags(f.Reset, f.Console)
		return a.Start(context.Background())
	}
}
