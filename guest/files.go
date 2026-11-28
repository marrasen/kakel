package guest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// The share's folder, as its guest reaches it: SFTP, on a channel of its
// own, to a server of the share's that knows nothing but that one folder.
//
// Every name is looked up through an os.Root on the folder, so no path,
// "..", absolute path or link leads above it, however the folder changes
// as it is looked up. What a guest may do is asked again for every
// request: the folder and FilesMay as they are then, not as they were when
// a file was opened. A file opened for more than is allowed now, or in a
// folder shared no more, is closed under the guest, which is told it
// failed.
//
// What is not here is refused: links of either kind, owners, permissions,
// sizes set but by writing, and every extension but posix-rename, which
// the file manager renames with.

// FilesMay is what a guest may do with the share's folder.
type FilesMay uint8

const (
	// FilesNone shares no files. The channel is refused, and on one opened
	// before, every request.
	FilesNone FilesMay = iota
	// FilesLook lists folders and reads files.
	FilesLook
	// FilesChange also creates, writes, renames and removes, and sets the
	// times a file has.
	FilesChange
)

// What one connection may have of the folder, so a guest can keep this
// machine no busier than somebody copying files would.
const (
	// filesAtOnce is how many files channels may be open at once.
	filesAtOnce = 2

	// mostHandles is how many files and folders may be open at once, of
	// all its channels.
	mostHandles = 64

	// mostAsked is how many requests on a channel may wait for their
	// answer. Each is at most sftpMost, so what the host holds of a
	// channel is a few megabytes, whatever the guest sends.
	mostAsked = 32
	sftpMost  = 256 << 10

	// Requests, and bytes of them, read from the guest: a request reads
	// at most 32 KB, so this is some 16 MB a second down and 8 MB up. Past
	// it the guest is read slower.
	fileAsksBurst, fileAsksRate   = 512, 512
	fileBytesBurst, fileBytesRate = 8 << 20, 8 << 20
)

// ErrNoFiles is a host that shares no files, or none now.
var ErrNoFiles = errors.New("no files are shared")

// FileOp is what a guest did in the share's folder.
type FileOp uint8

const (
	// FileDownloaded is a file read, FileUploaded one written, FileRenamed
	// one moved, FileRemoved one removed, and FolderCreated a folder made.
	FileDownloaded FileOp = iota + 1
	FileUploaded
	FileRenamed
	FileRemoved
	FolderCreated
)

// FileEvent is one thing a guest did in the share's folder, for the
// Window Log. The paths are the guest's own, from the top of the folder,
// with slashes, and To is where a file was renamed to. They are names as
// the guest wrote them, to be made plain where they are shown.
type FileEvent struct {
	Op       FileOp
	Path, To string
	// Folder says what was removed is a folder.
	Folder bool
	// Bytes is how much of a file was read or written.
	Bytes int64
}

// filesState is the folder shared and what may be done in it, of h.mu.
type filesState struct {
	dir  string
	root *os.Root
	// is is the folder root was opened on, to know it from another put
	// in its place under the same name.
	is  fs.FileInfo
	may FilesMay
	// gen counts the roots opened, so a file knows its folder is shared no
	// more.
	gen uint64
	// open are the files and folders open, of every connection.
	open map[*fileHandle]struct{}
	// parts are the part files the guest made in the folder and has not
	// renamed or removed, by their names in it: what a copy that stopped
	// short leaves, which goes as the folder is shared no more.
	parts map[string]struct{}
}

// isPart reports whether rel names a part file, as gunim's file manager
// writes a copy through one: .files-<number>.part.
func isPart(rel string) bool {
	name := filepath.Base(rel)
	return strings.HasPrefix(name, ".files-") && strings.HasSuffix(name, ".part")
}

// sweepParts removes from root the part files of parts the guest left,
// files only, as the folder is shared no more.
func sweepParts(root *os.Root, parts map[string]struct{}) {
	for rel := range parts {
		if info, err := root.Lstat(rel); err == nil && info.Mode().IsRegular() {
			_ = root.Remove(rel)
		}
	}
}

// SetFiles sets the folder a guest reaches, and what it may do there. An
// empty dir, or FilesNone, shares no files. Once it has returned, no
// request is begun that may does not allow, and nothing is begun in the
// folder before: every file that was open for more, or in another folder,
// is closed, and the guest is told so as it uses it. The error is a folder
// that could not be opened, and no files are shared then.
//
// It does not wait for the guest: a request already under way finishes,
// and nothing else does.
func (h *Host) SetFiles(dir string, may FilesMay) error {
	if dir == "" {
		may = FilesNone
	}
	h.filesMu.Lock()
	defer h.filesMu.Unlock()
	h.mu.Lock()
	open := may != FilesNone && (h.files.root == nil || dir != h.files.dir)
	is := h.files.is
	h.mu.Unlock()
	if !open && may != FilesNone {
		// The same name, and maybe another folder by now.
		now, err := os.Stat(dir)
		open = err != nil || is == nil || !os.SameFile(is, now)
	}
	var root *os.Root
	var err error
	if open {
		if root, err = os.OpenRoot(dir); err == nil {
			if is, err = root.Stat("."); err != nil {
				_ = root.Close()
				root = nil
			}
		}
		if err != nil {
			dir, may = "", FilesNone
		}
	}

	h.mu.Lock()
	if h.phase == PhaseOver {
		h.mu.Unlock()
		if root != nil {
			_ = root.Close()
		}
		return err
	}
	var shut *os.Root
	var parts map[string]struct{}
	switch {
	case root != nil:
		shut, h.files.root, h.files.is = h.files.root, root, is
		parts, h.files.parts = h.files.parts, nil
		h.files.gen++
	case may == FilesNone && h.files.root != nil:
		shut, h.files.root, h.files.is = h.files.root, nil, nil
		parts, h.files.parts = h.files.parts, nil
		h.files.gen++
	}
	h.files.dir, h.files.may = dir, may
	cut := h.cutLocked(func(f *fileHandle) bool { return f.gen != h.files.gen || f.need > may })
	h.mu.Unlock()
	for _, f := range cut {
		f.cut()
	}
	if shut != nil {
		sweepParts(shut, parts)
		_ = shut.Close()
	}
	return err
}

// cutLocked takes the files which says off those open, for the caller to
// close once it has let go of h.mu.
func (h *Host) cutLocked(which func(*fileHandle) bool) []*fileHandle {
	var cut []*fileHandle
	for f := range h.files.open {
		if which(f) {
			delete(h.files.open, f)
			cut = append(cut, f)
		}
	}
	return cut
}

// endFiles shares no more files, as the share ends.
func (h *Host) endFiles() {
	h.mu.Lock()
	root, parts := h.files.root, h.files.parts
	h.files = filesState{}
	cut := h.cutLocked(func(*fileHandle) bool { return true })
	h.mu.Unlock()
	for _, f := range cut {
		f.cut()
	}
	if root != nil {
		sweepParts(root, parts)
		_ = root.Close()
	}
}

// filesServer answers one channel's requests, as pkg/sftp's request
// server asks: its Handlers, all four.
type filesServer struct {
	c *hostConn
}

var (
	_ sftp.FileReader           = (*filesServer)(nil)
	_ sftp.OpenFileWriter       = (*filesServer)(nil)
	_ sftp.PosixRenameFileCmder = (*filesServer)(nil)
	_ sftp.LstatFileLister      = (*filesServer)(nil)
	_ sftp.ReadlinkFileLister   = (*filesServer)(nil)
)

// errDenied is a request that what the guest may do does not allow, or a
// name it may not have.
var errDenied = sftp.ErrSSHFxPermissionDenied

// now is the folder, as it is shared now, for a request that needs need.
func (s *filesServer) now(need FilesMay) (*os.Root, uint64, error) {
	h := s.c.h
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.phase != PhaseIn || h.cur != s.c || h.files.root == nil || h.files.may < need {
		return nil, 0, errDenied
	}
	return h.files.root, h.files.gen, nil
}

// still reports, as an error, whether f may be used now: it is closed
// once it may not.
func (s *filesServer) still(f *fileHandle) error {
	h := s.c.h
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.files.open[f]; !ok || h.phase != PhaseIn || h.cur != s.c {
		return errDenied
	}
	return nil
}

// local is a guest's path as a name inside the folder: "." for the top.
// The request server has made it whole and clean, so no ".." is left that
// leads above the top, and os.Root refuses whatever else would.
func local(p string) (string, error) {
	rel := strings.TrimPrefix(path.Clean("/"+p), "/")
	if rel == "" {
		return ".", nil
	}
	// A drive, or a separator the guest's slashes are not.
	if runtime.GOOS == "windows" && strings.ContainsAny(rel, `\:`) {
		return "", errDenied
	}
	return filepath.FromSlash(rel), nil
}

// shown is a guest's path as it is told of: whole, from the top.
func shown(p string) string { return path.Clean("/" + p) }

// open notes a file or folder opened for need, and reports whether there is
// room for it.
func (s *filesServer) open(f *fileHandle) bool {
	h := s.c.h
	h.mu.Lock()
	defer h.mu.Unlock()
	if s.c.handles >= mostHandles || h.files.gen != f.gen || h.files.root == nil || h.files.may < f.need ||
		h.phase != PhaseIn || h.cur != s.c {
		return false
	}
	s.c.handles++
	if h.files.open == nil {
		h.files.open = map[*fileHandle]struct{}{}
	}
	h.files.open[f] = struct{}{}
	return true
}

// part notes a part file the guest made in the folder of gen, or with
// made false that it is renamed or removed.
func (s *filesServer) part(rel string, gen uint64, made bool) {
	h := s.c.h
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case !made:
		delete(h.files.parts, rel)
	case gen == h.files.gen:
		if h.files.parts == nil {
			h.files.parts = map[string]struct{}{}
		}
		h.files.parts[rel] = struct{}{}
	}
}

// closed notes that f was closed by the guest, or with its channel.
func (s *filesServer) closed(f *fileHandle) {
	h := s.c.h
	h.mu.Lock()
	defer h.mu.Unlock()
	s.c.handles--
	delete(h.files.open, f)
}

// tell tells the host of what the guest did.
func (s *filesServer) tell(e FileEvent) {
	if s.c.h.cfg.OnFiles != nil {
		s.c.h.cfg.OnFiles(e)
	}
}

// hold opens the file of rel in root with flag for need, as a handle.
//
// Only a file, or a folder for dir, is opened: a pipe, a socket or a
// device could wait for ever as it is opened or read, or do something as
// it is. What is there is asked first, the name opened without waiting,
// and what was opened asked again, in case it was swapped meanwhile.
func (s *filesServer) hold(root *os.Root, gen uint64, p, rel string, flag int, need FilesMay, dir bool) (*fileHandle, error) {
	was, err := root.Stat(rel)
	made := errors.Is(err, fs.ErrNotExist) && flag&os.O_CREATE != 0
	switch {
	case made:
	case err != nil:
		return nil, bare(err)
	case !plainKind(was, dir):
		return nil, sftp.ErrSSHFxFailure
	}
	file, err := root.OpenFile(rel, flag|openNow, 0o666)
	if err != nil {
		return nil, bare(err)
	}
	is, err := file.Stat()
	if err != nil || !plainKind(is, dir) || !made && !os.SameFile(was, is) {
		_ = file.Close()
		return nil, sftp.ErrSSHFxFailure
	}
	f := &fileHandle{s: s, f: file, path: shown(p), need: need, gen: gen, made: made, trunc: flag&os.O_TRUNC != 0}
	if !s.open(f) {
		_ = file.Close()
		return nil, sftp.ErrSSHFxFailure
	}
	if made && isPart(rel) {
		s.part(rel, gen, true)
	}
	return f, nil
}

// plainKind reports whether info is a file, or a folder for dir.
func plainKind(info fs.FileInfo, dir bool) bool {
	if dir {
		return info.IsDir()
	}
	return info.Mode().IsRegular()
}

// Fileread opens a file to read.
func (s *filesServer) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	root, gen, err := s.now(FilesLook)
	if err != nil {
		return nil, err
	}
	rel, err := local(r.Filepath)
	if err != nil {
		return nil, err
	}
	f, err := s.hold(root, gen, r.Filepath, rel, os.O_RDONLY, FilesLook, false)
	if err != nil {
		return nil, err
	}
	f.reads = true
	return f, nil
}

// Filewrite opens a file to write.
func (s *filesServer) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	return s.write(r, os.O_WRONLY)
}

// OpenFile opens a file to read and write.
func (s *filesServer) OpenFile(r *sftp.Request) (sftp.WriterAtReaderAt, error) {
	return s.write(r, os.O_RDWR)
}

func (s *filesServer) write(r *sftp.Request, flag int) (*fileHandle, error) {
	root, gen, err := s.now(FilesChange)
	if err != nil {
		return nil, err
	}
	rel, err := local(r.Filepath)
	if err != nil {
		return nil, err
	}
	// Append is left out: each write says where it goes.
	how := r.Pflags()
	if how.Creat {
		flag |= os.O_CREATE
	}
	if how.Trunc {
		flag |= os.O_TRUNC
	}
	if how.Excl {
		flag |= os.O_EXCL
	}
	f, err := s.hold(root, gen, r.Filepath, rel, flag, FilesChange, false)
	if err != nil {
		return nil, err
	}
	f.reads, f.writes = flag&os.O_RDWR != 0, true
	return f, nil
}

// Filecmd does what changes a name.
func (s *filesServer) Filecmd(r *sftp.Request) error {
	switch r.Method {
	case "Setstat":
		return s.setstat(r)
	case "Rename":
		return s.rename(r, false)
	case "Remove", "Rmdir":
		return s.remove(r, r.Method == "Rmdir")
	case "Mkdir":
		return s.mkdir(r)
	}
	// Links, of either kind, and whatever else there may be.
	return sftp.ErrSSHFxOpUnsupported
}

// PosixRename renames, over a file that has the name already.
func (s *filesServer) PosixRename(r *sftp.Request) error { return s.rename(r, true) }

// setstat sets a file's times, and nothing else: no permissions, which
// could make a file the guest wrote one to run, or the folder open to
// every user of the machine; no owner; and no size, which a file has by
// what is written.
func (s *filesServer) setstat(r *sftp.Request) error {
	root, _, err := s.now(FilesChange)
	if err != nil {
		return err
	}
	rel, err := local(r.Filepath)
	if err != nil {
		return err
	}
	what := r.AttrFlags()
	if what.Size || what.UidGid || what.Permissions {
		return sftp.ErrSSHFxOpUnsupported
	}
	if !what.Acmodtime {
		return nil
	}
	at := r.Attributes()
	return bare(root.Chtimes(rel, time.Unix(int64(at.Atime), 0), time.Unix(int64(at.Mtime), 0)))
}

// rename renames, and only over nothing unless over says it may.
func (s *filesServer) rename(r *sftp.Request, over bool) error {
	root, _, err := s.now(FilesChange)
	if err != nil {
		return err
	}
	from, err := local(r.Filepath)
	if err != nil {
		return err
	}
	to, err := local(r.Target)
	if err != nil {
		return err
	}
	if from == "." || to == "." {
		return errDenied
	}
	if !over {
		if _, err := root.Lstat(to); !errors.Is(err, fs.ErrNotExist) {
			return sftp.ErrSSHFxFailure
		}
	}
	if err := root.Rename(from, to); err != nil {
		return bare(err)
	}
	s.part(from, 0, false)
	s.tell(FileEvent{Op: FileRenamed, Path: shown(r.Filepath), To: shown(r.Target)})
	return nil
}

// remove removes a file, or an empty folder for dir. A link is removed
// itself, and not what it leads to.
func (s *filesServer) remove(r *sftp.Request, dir bool) error {
	root, _, err := s.now(FilesChange)
	if err != nil {
		return err
	}
	rel, err := local(r.Filepath)
	if err != nil {
		return err
	}
	if rel == "." {
		return errDenied
	}
	info, err := root.Lstat(rel)
	if err != nil {
		return bare(err)
	}
	if info.IsDir() != dir {
		return sftp.ErrSSHFxFailure
	}
	if err := root.Remove(rel); err != nil {
		return bare(err)
	}
	s.part(rel, 0, false)
	s.tell(FileEvent{Op: FileRemoved, Path: shown(r.Filepath), Folder: dir})
	return nil
}

// mkdir creates a folder, with the permissions the machine gives one.
func (s *filesServer) mkdir(r *sftp.Request) error {
	root, _, err := s.now(FilesChange)
	if err != nil {
		return err
	}
	rel, err := local(r.Filepath)
	if err != nil {
		return err
	}
	if err := root.Mkdir(rel, 0o777); err != nil {
		return bare(err)
	}
	s.tell(FileEvent{Op: FolderCreated, Path: shown(r.Filepath)})
	return nil
}

// Filelist lists a folder, or says what one name is.
func (s *filesServer) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	root, gen, err := s.now(FilesLook)
	if err != nil {
		return nil, err
	}
	rel, err := local(r.Filepath)
	if err != nil {
		return nil, err
	}
	switch r.Method {
	case "Stat":
		info, err := root.Stat(rel)
		if err != nil {
			return nil, bare(err)
		}
		return oneInfo{bareInfo{info}}, nil
	case "List":
		f, err := s.hold(root, gen, r.Filepath, rel, os.O_RDONLY, FilesLook, true)
		if err != nil {
			return nil, err
		}
		return &dirLister{fileHandle: f, root: root, rel: rel}, nil
	}
	return nil, sftp.ErrSSHFxOpUnsupported
}

// Lstat says what one name is, a link as itself.
func (s *filesServer) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	root, _, err := s.now(FilesLook)
	if err != nil {
		return nil, err
	}
	rel, err := local(r.Filepath)
	if err != nil {
		return nil, err
	}
	info, err := root.Lstat(rel)
	if err != nil {
		return nil, bare(err)
	}
	return oneInfo{bareInfo{info}}, nil
}

// Readlink says where a link leads, where that is a place in the folder
// the link names from where it stands. Any other is the host's own, and
// may name its folders, its user or its drives: such a link reads as not
// there.
func (s *filesServer) Readlink(p string) (string, error) {
	root, _, err := s.now(FilesLook)
	if err != nil {
		return "", err
	}
	rel, err := local(p)
	if err != nil {
		return "", err
	}
	to, err := root.Readlink(rel)
	if err != nil {
		return "", bare(err)
	}
	to = filepath.ToSlash(to)
	if filepath.IsAbs(to) || filepath.VolumeName(to) != "" || path.IsAbs(to) || climbs(to) {
		return "", sftp.ErrSSHFxNoSuchFile
	}
	return to, nil
}

// climbs reports whether a link's words go up a folder anywhere. Where
// the link stands may be other than the guest's path to it says, through
// a folder that is a link itself, so up from it may be anywhere: only a
// target that goes down from it is surely inside.
func climbs(to string) bool {
	for part := range strings.SplitSeq(to, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// bare is err as the guest is told it: its kind, and none of the host's
// own words, which may name its paths.
func bare(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, io.EOF):
		return io.EOF
	case errors.Is(err, fs.ErrNotExist):
		return sftp.ErrSSHFxNoSuchFile
	case errors.Is(err, fs.ErrPermission):
		return errDenied
	case errors.Is(err, sftp.ErrSSHFxOpUnsupported):
		return sftp.ErrSSHFxOpUnsupported
	}
	return sftp.ErrSSHFxFailure
}

// bareInfo is what the guest is told of a file: no more than its name,
// size, kind, permissions and time, and not the owner's numbers on the
// host. A link has no size: its own is the length of where it leads.
type bareInfo struct{ fs.FileInfo }

func (bareInfo) Sys() any { return nil }

func (b bareInfo) Size() int64 {
	if b.Mode()&fs.ModeSymlink != 0 {
		return 0
	}
	return b.FileInfo.Size()
}

// oneInfo lists one name.
type oneInfo struct{ info fs.FileInfo }

func (o oneInfo) ListAt(ls []os.FileInfo, at int64) (int, error) {
	if at > 0 || len(ls) == 0 {
		return 0, io.EOF
	}
	ls[0] = o.info
	return 1, io.EOF
}

// fileHandle is a file or a folder the guest has open.
type fileHandle struct {
	s *filesServer
	f *os.File
	// path is the guest's name for it, need what it was opened for, and
	// gen the folder's.
	path string
	need FilesMay
	gen  uint64
	// made says it was created, trunc that it was emptied, and reads and
	// writes what it was opened to do.
	made, trunc, reads, writes bool
	read, wrote                atomic.Int64

	closing sync.Once
}

// ReadAt reads, while the file may be read.
func (f *fileHandle) ReadAt(p []byte, off int64) (int, error) {
	if !f.reads {
		return 0, errDenied
	}
	if err := f.s.still(f); err != nil {
		return 0, err
	}
	n, err := f.f.ReadAt(p, off)
	f.read.Add(int64(n))
	return n, bare(err)
}

// WriteAt writes, while the file may be written.
func (f *fileHandle) WriteAt(p []byte, off int64) (int, error) {
	if !f.writes {
		return 0, errDenied
	}
	if err := f.s.still(f); err != nil {
		return 0, err
	}
	n, err := f.f.WriteAt(p, off)
	f.wrote.Add(int64(n))
	return n, bare(err)
}

// cut closes the file under the guest, which is told so as it uses it.
// Only what is in a call already goes on.
func (f *fileHandle) cut() { _ = f.f.Close() }

// Close closes it, as the guest asks or its channel ends, and tells the
// host what was done with it.
func (f *fileHandle) Close() error {
	f.closing.Do(func() {
		f.s.closed(f)
		_ = f.f.Close()
		switch read, wrote := f.read.Load(), f.wrote.Load(); {
		case f.writes && (wrote > 0 || f.made || f.trunc):
			f.s.tell(FileEvent{Op: FileUploaded, Path: f.path, Bytes: wrote})
		case f.reads && read > 0:
			f.s.tell(FileEvent{Op: FileDownloaded, Path: f.path, Bytes: read})
		}
	})
	return nil
}

// dirLister lists a folder it has open, as it is read: a folder of any
// size is held a hundred names at a time.
type dirLister struct {
	*fileHandle
	root *os.Root
	rel  string
	at   int64
}

// ListAt lists the next names, which is all a guest is given: the request
// server asks for them in turn.
func (d *dirLister) ListAt(ls []os.FileInfo, at int64) (int, error) {
	if err := d.s.still(d.fileHandle); err != nil {
		return 0, err
	}
	if at != d.at {
		return 0, sftp.ErrSSHFxFailure
	}
	names, err := d.f.Readdirnames(len(ls))
	n := 0
	for _, name := range names {
		// Through the root, and not by the folder's own path, which may be
		// a link to elsewhere by now.
		info, err := d.root.Lstat(filepath.Join(d.rel, name))
		if err != nil {
			continue
		}
		ls[n] = bareInfo{info}
		n++
	}
	d.at += int64(n)
	if err != nil {
		return n, bare(err)
	}
	return n, nil
}

// files serves one channel to the folder, until it ends.
func (c *hostConn) files(ch ssh.Channel) {
	defer c.releaseFiles()
	m := &metered{rw: ch, ctx: c.ctx, asks: c.fileAsks, bytes: c.fileBytes, slots: make(chan struct{}, mostAsked)}
	s := &filesServer{c: c}
	srv := sftp.NewRequestServer(m, sftp.Handlers{FileGet: s, FilePut: s, FileCmd: s, FileList: s})
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve()
	}()
	// The connection's end is not held up by a request that waits, as on
	// a network drive that does not answer: what it has open is closed
	// under it, and it ends when it can.
	select {
	case <-served:
	case <-c.ctx.Done():
	}
	_ = srv.Close()
	c.h.mu.Lock()
	cut := c.h.cutLocked(func(f *fileHandle) bool { return f.s.c == c })
	c.h.mu.Unlock()
	for _, f := range cut {
		f.cut()
	}
}

// reserveFiles makes room for a channel to the folder.
func (c *hostConn) reserveFiles() error {
	h := c.h
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.phase != PhaseIn || h.cur != c || h.files.root == nil || h.files.may == FilesNone:
		return ErrNoFiles
	case c.filing >= filesAtOnce:
		return errors.New("too many files channels are open")
	}
	c.filing++
	return nil
}

func (c *hostConn) releaseFiles() {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	c.filing--
}

// metered is a files channel, read no faster than its limits, with no
// more than mostAsked requests unanswered: SFTP answers each request once,
// so one is free again as each answer is written. Both ways it counts by
// the length each packet begins with.
type metered struct {
	rw    io.ReadWriteCloser
	ctx   context.Context
	asks  *limit
	bytes *limit
	slots chan struct{}

	// in and out are where each way is in its packets.
	in, out packetAt
}

// packetAt is how far one way is through a packet: its length read so far,
// and then what is left of its body.
type packetAt struct {
	head [4]byte
	got  int
	left uint32
}

// starts reports whether a packet begins here.
func (p *packetAt) starts() bool { return p.got == 0 && p.left == 0 }

// step takes b, which goes past, and reports how many packets ended in it.
// It returns how much of b belongs to what it was at, the rest for the
// next call.
func (p *packetAt) step(b []byte) (used int, ended bool) {
	if p.got < len(p.head) {
		used = copy(p.head[p.got:], b)
		p.got += used
		if p.got == len(p.head) {
			p.left = uint32(p.head[0])<<24 | uint32(p.head[1])<<16 | uint32(p.head[2])<<8 | uint32(p.head[3])
			if p.left == 0 {
				p.got = 0
				return used, true
			}
		}
		return used, false
	}
	n := min(uint32(len(b)), p.left)
	p.left -= n
	if p.left == 0 {
		p.got = 0
		return int(n), true
	}
	return int(n), false
}

func (m *metered) Read(b []byte) (int, error) {
	if m.in.starts() {
		select {
		case m.slots <- struct{}{}:
		case <-m.ctx.Done():
			return 0, io.EOF
		}
		m.asks.wait(m.ctx, 1)
	}
	// No further than this packet's end, so the next is waited for.
	if m.in.got < len(m.in.head) {
		b = b[:min(len(b), len(m.in.head)-m.in.got)]
	} else {
		b = b[:min(uint32(len(b)), m.in.left)]
	}
	n, err := m.rw.Read(b)
	for rest := b[:n]; len(rest) > 0; {
		used, _ := m.in.step(rest)
		rest = rest[used:]
	}
	m.bytes.wait(m.ctx, float64(n))
	return n, err
}

func (m *metered) Write(b []byte) (int, error) {
	n, err := m.rw.Write(b)
	for rest := b[:n]; len(rest) > 0; {
		used, ended := m.out.step(rest)
		rest = rest[used:]
		if ended {
			select {
			case <-m.slots:
			default:
			}
		}
	}
	return n, err
}

func (m *metered) Close() error { return m.rw.Close() }

// Files opens the share's folder, as an SFTP client: its top is the
// folder's, and nothing above it is there. What it may do is what the host
// allows as each request comes, and anything else fails, as does a file
// the host closed under it. The error is ErrNoFiles where the host shares
// none, or ErrLost.
//
// The caller closes the client, which leaves the session open.
func (s *Session) Files(ctx context.Context) (*sftp.Client, error) {
	ch, reqs, err := openChannel(ctx, s.conn, s.gone, chanFiles, nil)
	var no *ssh.OpenChannelError
	switch {
	case ctx.Err() != nil && err != nil:
		return nil, ctx.Err()
	case errors.As(err, &no):
		return nil, ErrNoFiles
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrLost, err)
	}
	go ssh.DiscardRequests(reqs)
	// The host answers at once, or the channel ends.
	stop := context.AfterFunc(ctx, func() { _ = ch.Close() })
	defer stop()
	c, err := sftp.NewClientPipe(ch, ch)
	if err != nil {
		_ = ch.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %w", ErrLost, err)
	}
	return c, nil
}
