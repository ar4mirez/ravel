package vm

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/u-root/u-root/pkg/cpio"
	"github.com/valyentdev/ravel-init/vminit"
	"github.com/valyentdev/ravel/core/instance"
	"github.com/valyentdev/ravel/pkg/cloudhypervisor"
	"golang.org/x/sys/unix"
)

func (b *Builder) writeInitrd(file *os.File, instance *instance.Instance, image v1.Image) error {
	slog.Debug("writing initrd", "instance", instance.Id)

	t1 := time.Now()
	// init, err := os.Open(b.initBinary)
	// if err != nil {
	// 	return fmt.Errorf("failed to read init binary: %w", err)
	// }
	// defer init.Close()

	slog.Info("init binary opened after", "time", time.Since(t1))

	gz := gzip.NewWriter(file)
	defer gz.Close()

	w := cpio.Newc.Writer(gz)

	config := getInitConfig(instance, image.Config)
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal init config: %w", err)
	}

	// initInfos, err := init.Stat()
	// if err != nil {
	// 	return fmt.Errorf("failed to get init stat: %w", err)
	// }

	configRecord := cpio.StaticFile("/ravel/run.json", string(configJSON), 0644)

	init := bytes.NewReader(b.initBin)
	initRecord := cpio.Record{
		ReaderAt: init,
		Info: cpio.Info{
			FileSize: uint64(len(b.initBin)),
			Name:     "ravel-init",
			Mode:     unix.S_IFREG | 0755,
		},
	}

	slog.Info("init and config records created after", "time", time.Since(t1))
	err = cpio.WriteRecordsAndDirs(w, []cpio.Record{initRecord, configRecord})
	if err != nil {
		return fmt.Errorf("failed to write records and dirs: %w", err)
	}

	slog.Info("initrd written after", "time", time.Since(t1))

	return nil
}

func getInitConfig(instance *instance.Instance, image v1.ImageConfig) vminit.Config {
	config := instance.Config

	return vminit.Config{
		ImageConfig: &vminit.ImageConfig{
			User:       cloudhypervisor.StringPtr(image.User),
			WorkingDir: cloudhypervisor.StringPtr(image.WorkingDir),
			Cmd:        image.Cmd,
			Entrypoint: image.Entrypoint,
			Env:        image.Env,
		},
		UserOverride:       config.Init.User,
		CmdOverride:        config.Init.Cmd,
		EntrypointOverride: config.Init.Entrypoint,
		RootDevice:         "/dev/vda",
		EtcResolv: vminit.EtcResolv{
			Nameservers: []string{"8.8.8.8"},
		},
		ExtraEnv: config.Env,
		Network: vminit.NetworkConfig{
			IPConfigs: []vminit.IPConfig{
				{
					IPNet:     instance.Network.Local.InstanceIPNet().String(),
					Broadcast: instance.Network.Local.Broadcast.String(),
					Gateway:   instance.Network.Local.Gateway.String(),
				},
			},
			DefaultGateway: instance.Network.Local.Gateway.String(),
		},
	}
}
