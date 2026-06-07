package shell

import (
	"fmt"
	"os/exec"
	"strings"
)

type Bash struct{}

func (s Bash) RunCommand(ex string, args []string) ([]byte, error) {
	cmd := exec.Command(ex, "-lc", strings.Join(args, " "))

	path, err := exec.LookPath(ex)
	if err != nil {
		return nil, err
	}

	fmt.Println(path)
	fmt.Println(cmd.Args)

	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return out, nil
}
