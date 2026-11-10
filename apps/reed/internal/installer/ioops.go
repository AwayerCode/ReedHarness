package installer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	if err := output.Chmod(mode.Perm()); err != nil {
		output.Close()
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func checkParents(projectRoot, target string) error {
	relative, err := filepath.Rel(projectRoot, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q is outside the project", target)
	}
	parts := strings.Split(relative, string(filepath.Separator))
	cursor := projectRoot
	for index := 0; index < len(parts)-1; index++ {
		cursor = filepath.Join(cursor, parts[index])
		info, err := os.Lstat(cursor)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect parent %q: %w", cursor, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("parent %q is a symbolic link; refusing to write outside the project", cursor)
		}
		if !info.IsDir() {
			return fmt.Errorf("parent %q is not a directory", cursor)
		}
	}
	return nil
}

func mkdirAllTracked(projectRoot, directory string) ([]string, error) {
	relative, err := filepath.Rel(projectRoot, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("directory %q is outside the project", directory)
	}
	cursor := projectRoot
	created := make([]string, 0)
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		cursor = filepath.Join(cursor, part)
		info, err := os.Lstat(cursor)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(cursor, 0o755); err != nil {
				return created, fmt.Errorf("create directory %q: %w", cursor, err)
			}
			created = append(created, cursor)
			continue
		}
		if err != nil {
			return created, fmt.Errorf("inspect directory %q: %w", cursor, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return created, fmt.Errorf("directory %q is a symbolic link", cursor)
		}
		if !info.IsDir() {
			return created, fmt.Errorf("path %q is not a directory", cursor)
		}
	}
	return created, nil
}
