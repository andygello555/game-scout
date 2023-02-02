package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/pkg/errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PathsToBytesInterface encapsulates the base behaviour of a directory structure stored as a map in memory.
type PathsToBytesInterface interface {
	BaseDir() string
	Path(filename string) string
	AddPathBytes(path string, data []byte)
	AddFilenameBytes(filename string, data []byte)
	Len() int
}

func pathDefault(ftb PathsToBytesInterface, filename string) string {
	return filepath.Join(ftb.BaseDir(), filename)
}

func addFilenameBytesDefault(ftb PathsToBytesInterface, filename string, data []byte) {
	ftb.AddPathBytes(ftb.Path(filename), data)
}

// PathsToBytesReader encapsulates the behaviour of reading a directory structure stored in whatever medium the
// implementor wishes.
type PathsToBytesReader interface {
	PathsToBytesInterface
	LoadBaseDir() (err error)
	ReadPath(path string) (err error)
	BytesForFilename(filename string) ([]byte, error)
	BytesForDirectory(directory string) *PathsToBytesDirectory
}

// PathsToBytesWriter encapsulates the behaviour of writing a directory structure stored as a map in memory to whatever
// medium the implementor wishes.
type PathsToBytesWriter interface {
	PathsToBytesInterface
	Write() (err error)
}

// PathsToBytesReadWriter encapsulates the behaviour of both PathsToBytesReader and PathsToBytesWriter.
type PathsToBytesReadWriter interface {
	PathsToBytesReader
	PathsToBytesWriter
}

// PathsToBytesDirectory acts as a lookup for paths to files on the disk to the bytes read from said files. This can
// represent both a read or a write process, hence it implements PathsToBytesReadWriter.
type PathsToBytesDirectory struct {
	inner map[string][]byte
	// baseDir is the path to the directory that all paths within the PathsToBytesDirectory will be relative to. This
	// can be an absolute path or a relative path. All paths within the lookup will have this directory as a prefix.
	baseDir string
}

// NewPathsToBytes creates a new PathsToBytesDirectory for the given directory.
func NewPathsToBytes(baseDir string) *PathsToBytesDirectory {
	return &PathsToBytesDirectory{
		inner:   make(map[string][]byte),
		baseDir: baseDir,
	}
}

// BaseDir is a getter for the base directory.
func (ftb *PathsToBytesDirectory) BaseDir() string { return ftb.baseDir }

// LoadBaseDir will recursively load all the files within the BaseDir into the lookup.
func (ftb *PathsToBytesDirectory) LoadBaseDir() (err error) {
	if err = filepath.Walk(
		ftb.baseDir,
		func(path string, info os.FileInfo, err error) error {
			log.INFO.Printf("PathsToBytesDirectory has visited %s (file = %t)", path, !info.IsDir())
			if err != nil {
				return err
			}
			if !info.IsDir() {
				if err = ftb.ReadPath(path); err != nil {
					return err
				}
			}
			return nil
		},
	); err != nil {
		err = errors.Wrapf(err, "could not read directory \"%s\" into PathsToBytesDirectory", ftb.baseDir)
	}
	return
}

// ReadPath will read the file at the given path into the lookup.
func (ftb *PathsToBytesDirectory) ReadPath(path string) (err error) {
	var data []byte
	if data, err = os.ReadFile(path); err != nil {
		err = errors.Wrapf(err, "cannot read file \"%s\" into PathsToBytesDirectory", path)
		return
	}
	ftb.AddPathBytes(path, data)
	return
}

// Path returns the BaseDir joined with the given filename, or in other words a path that is relative to the BaseDir.
func (ftb *PathsToBytesDirectory) Path(filename string) string {
	return pathDefault(ftb, filename)
}

// AddPathBytes will create a lookup entry for bytes read from a given path. The path is relative to BaseDir.
func (ftb *PathsToBytesDirectory) AddPathBytes(path string, data []byte) {
	ftb.inner[path] = data
}

// AddFilenameBytes will create a lookup entry for bytes read from a given filename. This filename will first be normalised
// to a path that is relative to the BaseDir, and then AddPathBytes will be called.
func (ftb *PathsToBytesDirectory) AddFilenameBytes(filename string, data []byte) {
	addFilenameBytesDefault(ftb, filename, data)
}

// Write will write all the paths in the lookup to the disk.
func (ftb *PathsToBytesDirectory) Write() (err error) {
	start := time.Now().UTC()
	log.INFO.Printf("PathsToBytesDirectory is writing %d files to disk", len(ftb.inner))
	for filename, data := range ftb.inner {
		log.INFO.Printf("\tWriting %d bytes to file %s", len(data), filename)
		// Create the parent directories for this file
		if _, err = os.Stat(filepath.Dir(filename)); os.IsNotExist(err) {
			if err = os.MkdirAll(filepath.Dir(filename), dirPerms); err != nil && !os.IsExist(err) {
				err = errors.Wrapf(err, "could not create parent directories for \"%s\"", filename)
			}
		}

		// Create the file descriptor
		if err = os.WriteFile(filename, data, filePerms); err != nil {
			err = errors.Wrapf(err, "could not write bytes to file \"%s\" in PathsToBytesDirectory", filename)
			return
		}
	}
	log.INFO.Printf(
		"PathsToBytesDirectory has finished writing %d files to disk in %s",
		len(ftb.inner), time.Now().UTC().Sub(start).String(),
	)
	return
}

// BytesForFilename looks up the given filename in the lookup, after converting the filename to a path relative to the
// BaseDir.
func (ftb *PathsToBytesDirectory) BytesForFilename(filename string) ([]byte, error) {
	filename = ftb.Path(filename)
	if data, ok := ftb.inner[filename]; ok {
		return data, nil
	} else {
		return data, fmt.Errorf("%s does not exist in PathsToBytesDirectory", filename)
	}
}

// BytesForDirectory lookups up files that have the given directory as a prefix. This is done after converting the
// directory name to a path relative to the BaseDir.
func (ftb *PathsToBytesDirectory) BytesForDirectory(directory string) *PathsToBytesDirectory {
	ftbOut := NewPathsToBytes(ftb.Path(directory))
	for path, data := range ftb.inner {
		if strings.HasPrefix(path, ftbOut.baseDir) {
			ftbOut.AddPathBytes(path, data)
		}
	}
	return ftbOut
}

// Len returns the number of files in the lookup.
func (ftb *PathsToBytesDirectory) Len() int {
	return len(ftb.inner)
}

type pathToBytesZipped struct {
	inner   map[string][]byte
	baseDir string
	buf     *bytes.Buffer
	archive *zip.Writer
}

func (p *pathToBytesZipped) BaseDir() string                       { return p.baseDir }
func (p *pathToBytesZipped) Path(filename string) string           { return pathDefault(p, filename) }
func (p *pathToBytesZipped) AddPathBytes(path string, data []byte) { p.inner[path] = data }
func (p *pathToBytesZipped) Len() int                              { return len(p.inner) }
func (p *pathToBytesZipped) AddFilenameBytes(filename string, data []byte) {
	addFilenameBytesDefault(p, filename, data)
}

func (p *pathToBytesZipped) Write() (err error) {
	start := time.Now().UTC()
	p.buf = new(bytes.Buffer)
	p.archive = zip.NewWriter(p.buf)
	defer func(archive *zip.Writer) {
		err = myErrors.MergeErrors(err, archive.Close())
	}(p.archive)

	log.INFO.Printf("PathsToBytesZipped is writing %d files to zip", p.Len())
	for filename, data := range p.inner {
		log.INFO.Printf("\tWriting %d bytes to file %s", len(data), filename)
		var w io.Writer
		if w, err = p.archive.Create(filename); err != nil {
			err = errors.Wrapf(err, "could not create archived file %q", filename)
		}

		if _, err = w.Write(data); err != nil {
			err = errors.Wrapf(err, "could not write %d bytes to file %q in PathsToBytesZipped", len(data), filename)
			return
		}
	}

	log.INFO.Printf(
		"PathsToBytesZipped has finished writing %d files to archive in %s",
		p.Len(), time.Now().UTC().Sub(start).String(),
	)
	return
}

func newPathsToBytesZipped() *pathToBytesZipped {
	return &pathToBytesZipped{
		inner:   make(map[string][]byte),
		baseDir: "",
		buf:     nil,
		archive: nil,
	}
}
