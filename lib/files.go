// +build windows

package lib

import (
	"context"
	"mime"
	"os"
	"path"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/net/webdav"
)

type Dir struct {
	webdav.Dir
	noSniff bool
}

func (d Dir) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	if !d.noSniff {
		return d.Dir.Stat(ctx, name)
	}

	info, err := d.Dir.Stat(ctx, name)
	if err != nil {
		return nil, err
	}

	return noSniffFileInfo{info}, nil
}

func (d Dir) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	fullPath := filepath.Join(string(d.Dir), name)

	// Captura o timestamp original ANTES de abrir o arquivo
	var origCTime, origATime, origMTime syscall.Filetime
	hasOrigTime := getFileTimes(fullPath, &origCTime, &origATime, &origMTime)

	file, err := d.Dir.OpenFile(ctx, name, flag, perm)
	if err != nil {
		return nil, err
	}

	nf := &noSniffFile{
		File:        file,
		fullPath:    fullPath,
		hasOrigTime: hasOrigTime,
		origCTime:   origCTime,
		origATime:   origATime,
		origMTime:   origMTime,
		flag:        flag,
	}

	return nf, nil
}

// -----------------------
//  STRUCTS SEM ALTERAÇÃO
// -----------------------

type noSniffFileInfo struct {
	os.FileInfo
}

func (w noSniffFileInfo) ContentType(ctx context.Context) (contentType string, err error) {
	if mimeType := mime.TypeByExtension(path.Ext(w.FileInfo.Name())); mimeType != "" {
		return mimeType, nil
	}
	return "application/octet-stream", nil
}

// -----------------------
//  FILE COM PROTEÇÃO DE DATA
// -----------------------

type noSniffFile struct {
	webdav.File

	fullPath    string
	hasOrigTime bool
	origCTime   syscall.Filetime
	origATime   syscall.Filetime
	origMTime   syscall.Filetime
	flag        int
}

func (f noSniffFile) Stat() (os.FileInfo, error) {
	info, err := f.File.Stat()
	if err != nil {
		return nil, err
	}

	return noSniffFileInfo{info}, nil
}

func (f noSniffFile) Readdir(count int) ([]os.FileInfo, error) {
	fis, err := f.File.Readdir(count)
	if err != nil {
		return nil, err
	}

	for i := range fis {
		fis[i] = noSniffFileInfo{fis[i]}
	}
	return fis, nil
}

// -------------------------
// RESTAURA A DATA NO FECHAMENTO
// -------------------------

func (f *noSniffFile) Close() error {
	err := f.File.Close()

	// Se arquivo nunca existiu antes, não restaura datas
	if !f.hasOrigTime {
		return err
	}

	// Restaura timestamps originais se houve escrita/truncamento
	if f.flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		restoreFileTimes(f.fullPath, f.origCTime, f.origATime, f.origMTime)
	}

	return err
}

// ----------------------------
// WINDOWS: LEITURA DE TIMESTAMPS
// ----------------------------

func getFileTimes(path string, c, a, m *syscall.Filetime) bool {
	handle, err := syscall.CreateFile(
		syscall.StringToUTF16Ptr(path),
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)

	err = syscall.GetFileTime(handle, c, a, m)
	return err == nil
}

// ----------------------------
// WINDOWS: RESTAURA TIMESTAMPS
// ----------------------------

func restoreFileTimes(path string, c, a, m syscall.Filetime) {
	handle, err := syscall.CreateFile(
		syscall.StringToUTF16Ptr(path),
		syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return
	}
	defer syscall.CloseHandle(handle)

	syscall.SetFileTime(handle, &c, &a, &m)
}

