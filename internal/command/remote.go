package command

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/client"
	"github.com/servak/mping/internal/server"
)

const socketScheme = "unix://"

// parseAddress returns the path of the socket of an address.
func parseAddress(address string) (string, error) {
	path, ok := strings.CutPrefix(address, socketScheme)
	if path = filepath.Clean(path); !ok || !filepath.IsAbs(path) || path == "/" {
		return "", fmt.Errorf("invalid server address %q: it must be unix:///PATH", address)
	}
	return path, nil
}

func checkPlatform(*cobra.Command, []string) error {
	if runtime.GOOS == "windows" {
		return errors.New("mping server and mping watch are not supported on windows")
	}
	return nil
}

func addServerFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().StringP("server", "s", "", "address of the server, unix:///PATH (default $MPING_SERVER, else unix://~/.mping/server.sock)")
}

// remote is the server that a command talks to.
type remote struct {
	*client.Client
	isDefault bool // on the socket of the user
	given     bool // by -s or MPING_SERVER
}

// newRemote returns the server of --server, MPING_SERVER or else the default
// one.
func newRemote(cmd *cobra.Command) (*remote, error) {
	socket, defaultErr := server.DefaultSocket()
	r := &remote{isDefault: true}
	if address := os.Getenv("MPING_SERVER"); address != "" || cmd.Flags().Changed("server") {
		if cmd.Flags().Changed("server") {
			address, _ = cmd.Flags().GetString("server")
		}
		given, err := parseAddress(address)
		if err != nil {
			return nil, err
		}
		r.given, r.isDefault, socket = true, defaultErr == nil && given == socket, given
	} else if defaultErr != nil {
		return nil, defaultErr
	}
	r.Client = client.New(socket)
	return r, nil
}

// announce says which server it is unless it is the usual one.
func (r *remote) announce(w io.Writer) {
	if !r.isDefault {
		fmt.Fprintf(w, "server: %s%s\n", socketScheme, api.Clean(r.Socket, 1000))
	}
}

// warnVersion warns if the server that answered is of another version. Builds
// without a version are not compared.
func (r *remote) warnVersion(w io.Writer, version string) {
	if v := r.ServerVersion; v != "" && version != "" && v != version {
		fmt.Fprintf(w, "warning: the server is version %s, this is %s; \"mping server stop\" ends the old one\n", api.Clean(v, 50), version)
	}
}

// explain returns err with all that its reader should know.
func (r *remote) explain(err error) error {
	var e *api.Error
	switch {
	case errors.Is(err, client.ErrNotRunning) && !r.given:
		return fmt.Errorf("%w; \"mping watch start\" starts it", client.ErrNotRunning)
	case errors.As(err, &e) && len(e.Targets) > 0:
		var b strings.Builder
		for _, t := range e.Targets {
			fmt.Fprintf(&b, "\n  %s: %s", api.Clean(t.Target, 300), api.Clean(t.Message, 300))
		}
		return fmt.Errorf("%w:%s", err, b.String())
	}
	return err
}
