package ztest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func RunShell(ctx context.Context, dir, bindir, script string, stdin io.Reader, useenvs, extraenvs []string) (string, string, error) {
	// "-e -o pipefile" ensures a test will fail if any command
	// fails unexpectedly.
	cmd := exec.CommandContext(ctx, "bash", "-e", "-o", "pipefail", "-c", script)
	cmd.Dir = dir
	cmd.Env = EnvForHomeDirAndPath(dir, bindir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, env := range useenvs {
		if v, ok := os.LookupEnv(env); ok {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", env, v))
		}
	}
	cmd.Env = append(cmd.Env, extraenvs...)
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func EnvForHomeDirAndPath(dir, path string) []string {
	return []string{
		"PATH=" + path,
		// Unix
		"HOME=" + dir,            // os.UserCacheDir, os.UserConfigDir, os.UserHomeDir
		"TMPDIR=" + os.TempDir(), // os.TempDir
		// Windows
		"AppData=" + dir,      // os.UserConfigDir
		"LocalAppData=" + dir, // os.UserCacheDir
		"TMP=" + os.TempDir(), // os.TempDir
		"USERPROFILE=" + dir,  // os.UserHomeDir
	}
}
