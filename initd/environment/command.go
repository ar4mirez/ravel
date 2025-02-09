package environment

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/valyentdev/ravel/initd"
)

func buildCommand(cfg initd.Config) (*exec.Cmd, error) {
	var entrypoint []string
	var cmd []string

	if cfg.EntrypointOverride != nil {
		entrypoint = cfg.EntrypointOverride
	} else {
		entrypoint = cfg.ImageConfig.Entrypoint
	}

	if cfg.CmdOverride != nil {
		cmd = cfg.CmdOverride
	} else {
		cmd = cfg.ImageConfig.Cmd
	}

	args := append(entrypoint, cmd...)

	envars := append(cfg.ImageConfig.Env, cfg.ExtraEnv...)

	if err := populateProcessEnv(envars); err != nil {
		return nil, fmt.Errorf("error populating process env: %v", err)
	}

	workingDir := "/"
	if cfg.ImageConfig.WorkingDir != nil {
		workingDir = *cfg.ImageConfig.WorkingDir
	}

	err := os.Chdir(workingDir)
	if err != nil {
		return nil, fmt.Errorf("error changing working directory: %v", err)
	}

	lp, err := exec.LookPath(args[0])
	if err != nil {
		return nil, fmt.Errorf("error searching for executable: %v", err)
	}

	command := &exec.Cmd{
		Path:   lp,
		Args:   args,
		Env:    envars,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}

	return command, nil
}
