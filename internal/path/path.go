package path

import (
	"io/fs"
)

type WalkDir struct {
	path string
}

func New(p string) *WalkDir {
	return &WalkDir{
		path: p,
	}
}

func (wd *WalkDir) GetPath() string {
	return wd.path
}

func (wd *WalkDir) Walk(path string, info fs.FileInfo, err error) error {
	if err != nil {
		return err
	}

	if !info.IsDir() {
		// fmt.Println(path)
	}

	return nil
}
