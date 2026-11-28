//go:build !plan9

// Package extract implements 'rclone archive extract'
package extract

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/rclone/rclone/cmd"
	"github.com/rclone/rclone/cmd/archive"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	"github.com/rclone/rclone/fs/config/flags"
	"github.com/rclone/rclone/fs/filter"
	"github.com/rclone/rclone/fs/operations"
	"github.com/rclone/rclone/lib/sanitize"
	"github.com/spf13/cobra"
)

var (
	archivePassword     string
	archiveWindowSize   int64
	archiveSkipExisting bool

	errPasswordUnsupported = errors.New("--archive-password is only supported for RAR and 7z archives")
)

const defaultWindowSize = 16 << 20

// ExtractOptions configures an ArchiveExtract call.
type ExtractOptions struct {
	Password     string
	WindowSize   int64
	SkipExisting bool
}

// DefaultExtractOptions returns the options used when none are supplied.
func DefaultExtractOptions() ExtractOptions {
	return ExtractOptions{WindowSize: defaultWindowSize}
}

func init() {
	flagSet := Command.Flags()
	flags.StringVarP(flagSet, &archivePassword, "archive-password", "", "", "Password for encrypted RAR and 7z archives", "")
	flags.Int64VarP(flagSet, &archiveWindowSize, "archive-window-size", "", defaultWindowSize, "Sliding window cache size in bytes for random-access archives (ZIP, 7z, ISO9660); 0 disables caching", "")
	flags.BoolVarP(flagSet, &archiveSkipExisting, "archive-skip-existing", "", true, "Skip entries already present at the destination with the same size", "")
	archive.Command.AddCommand(Command)
}

// Command - extract
var Command = &cobra.Command{
	Use:   "extract [flags] <source> <destination>",
	Short: `Extract archives from source to destination.`,
	Long: strings.ReplaceAll(`

Extract the archive contents to a destination directory auto detecting
the format. See [rclone archive create](/commands/rclone_archive_create/)
for the archive formats supported.

For example on this archive:

|||
$ rclonex archive list --long remote:archive.zip
        6 2025-10-30 09:46:23.000000000 file.txt
        0 2025-10-30 09:46:57.000000000 dir/
        4 2025-10-30 09:46:57.000000000 dir/bye.txt
|||

You can run extract like this

|||
$ rclonex archive extract remote:archive.zip remote:extracted
|||

Which gives this result

|||
$ rclonex tree remote:extracted
/
├── dir
│   └── bye.txt
└── file.txt
|||

The source or destination or both can be local or remote. The source
archive is streamed - it is not downloaded to the local machine first.
Nothing is mounted or staged on local disk. Remotes which do not support
range reads fail with an explicit error rather than silently extracting
corrupted files.

All formats are decoded by the bundled 7-Zip engine. RAR and 7z archives
can be encrypted - use --archive-password to set the password. The password
is not stored in the config file.

Multi-volume RAR and 7z archives (and encrypted multi-volume sets with
a password) are supported: the volumes are streamed from the remote in order,
nothing is staged locally. Spanned/multi-disk ZIPs and multi-part TAR archives
are not supported.

--archive-window-size sets the size of the ranged read cache used for remote
objects (16 MiB by default; 0 reads one byte at a time). For nested archives
such as tar.gz the engine re-reads the decompressed stream on backward seeks
and buffers at most RCLONEX_ARCHIVE_BUFFER_SIZE bytes (64 MiB by default;
minimum 1 MiB). Memory usage also grows with the number of archive entries
and with the dictionary size declared by 7z archives.

By default, entries whose destination object already exists with the same
size are left untouched. This lets a retry continue an interrupted extraction
without re-uploading completed files. Pass --archive-skip-existing=false to
extract over existing files.

The [archive backend](/archive/) can also be used to extract files. It
can be used to read only mount archives also but it supports a
different set of archive formats to the archive commands.
`, "|", "`"),

	Annotations: map[string]string{
		"versionIntroduced": "v1.72",
	},
	RunE: func(command *cobra.Command, args []string) error {
		cmd.CheckArgs(2, 2, command, args)

		opts := ExtractOptions{
			Password:     archivePassword,
			WindowSize:   archiveWindowSize,
			SkipExisting: archiveSkipExisting,
		}

		src, srcFile := cmd.NewFsFile(args[0])
		dst, dstFile := cmd.NewFsFile(args[1])

		cmd.Run(false, false, command, func() error {
			return ArchiveExtract(context.Background(), dst, dstFile, src, srcFile, opts)
		})
		return nil
	},
}

func withArchiveStream(ctx context.Context, srcObj fs.Object, tr *accounting.Transfer, options []fs.OpenOption, fn func(in accounting.AccountReadAtSeeker) error) error {
	in0, err := operations.Open(ctx, srcObj, options...)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", srcObj.Remote(), err)
	}
	defer func() {
		_ = in0.Close()
	}()
	acc := tr.Account(ctx, in0)
	in, err := acc.WithReadAtSeeker()
	if err != nil {
		return err
	}
	return fn(in)
}

// ArchiveExtract extracts files from (src, srcFile) to (dst, dstDir) using opts.
func ArchiveExtract(ctx context.Context, dst fs.Fs, dstDir string, src fs.Fs, srcFile string, opts ExtractOptions) error {
	var srcObj fs.Object
	var filesExtracted = 0
	var err error

	fi := filter.GetConfig(ctx)
	ci := fs.GetConfig(ctx)
	srcObj, err = src.NewObject(ctx, srcFile)
	fs.Debugf(nil, "srcFile: %q, src: %v", srcFile, src)
	if errors.Is(err, fs.ErrorIsDir) {
		return fmt.Errorf("source can't be a directory: %w", err)
	} else if errors.Is(err, fs.ErrorObjectNotFound) {
		return fmt.Errorf("source not found: %w", err)
	} else if err != nil {
		return fmt.Errorf("unable to access source: %w", err)
	}
	fs.Debugf(nil, "Source archive file: %s/%s", src.Root(), srcFile)
	err = dst.Mkdir(ctx, dstDir)
	if err != nil {
		return fmt.Errorf("unable to access destination: %w", err)
	}

	fs.Debugf(dst, "Destination for extracted files: %q", dstDir)
	tr := accounting.Stats(ctx).NewTransfer(srcObj, nil)
	defer tr.Done(ctx, err)
	var options []fs.OpenOption
	for _, option := range fs.GetConfig(ctx).DownloadHeaders {
		options = append(options, option)
	}

	newEntryHandler := func(dir string) entryHandler {
		return func(nameInArchive string, size int64, modTime time.Time, isDir bool, open func() (io.ReadCloser, error)) error {
			remote, err := destPath(nameInArchive, dir)
			if err != nil {
				return err
			}
			if remote == "" {
				return nil
			}
			if !fi.Include(remote, size, modTime, fs.Metadata{}) {
				return nil
			}
			if opts.SkipExisting && size >= 0 {
				if obj, err := dst.NewObject(ctx, remote); err == nil && obj.Size() == size {
					fs.Debugf(nil, "Skipping existing %s", remote)
					return nil
				}
			}
			if isDir {
				fs.Debugf(nil, "mkdir %s", remote)
				if ci.DryRun {
					return nil
				}
				return operations.Mkdir(ctx, dst, remote)
			}
			fs.Debugf(nil, "Extract %s", remote)
			if ci.DryRun {
				filesExtracted++
				return nil
			}
			err = uploadEntry(ctx, dst, remote, nameInArchive, size, modTime, open)
			if err == nil {
				filesExtracted++
			}
			return err
		}
	}
	handleEntry := newEntryHandler(dstDir)

	err = withArchiveStream(ctx, srcObj, tr, options, func(in accounting.AccountReadAtSeeker) error {
		if opts.Password != "" && !lib7zipSupportsPassword(srcFile) {
			return errPasswordUnsupported
		}
		err := extractWithLib7zip(lib7zipArgs{
			ctx:        ctx,
			src:        src,
			srcObj:     srcObj,
			srcFile:    srcFile,
			windowSize: opts.WindowSize,
			tr:         tr,
			password:   opts.Password,
			options:    options,
			handle:     handleEntry,
		})
		fs.Infof(nil, "Total files extracted %d", filesExtracted)
		return err
	})
	return err
}

type shortEntryError struct {
	name string
	got  int64
	want int64
}

func (e *shortEntryError) Error() string {
	return fmt.Sprintf("archive entry %q: extracted %d bytes, expected %d (archive may be corrupt)", e.name, e.got, e.want)
}

// uploadEntry streams one archive entry into dst and removes the destination
// object when the upload fails or yields fewer bytes than the archive declares.
func uploadEntry(ctx context.Context, dst fs.Fs, remote, nameInArchive string, size int64, modTime time.Time, open func() (io.ReadCloser, error)) error {
	fin, err := open()
	if err != nil {
		return err
	}
	counter := &countingReadCloser{ReadCloser: fin}
	// Rcat creates its own per-file transfer for the upload
	obj, err := operations.Rcat(ctx, dst, remote, counter, modTime, nil)
	if err != nil {
		removePartial(ctx, remote, obj)
		return fmt.Errorf("archive entry %q: %w", nameInArchive, err)
	}
	if size >= 0 && counter.n != size {
		removePartial(ctx, remote, obj)
		return &shortEntryError{name: nameInArchive, got: counter.n, want: size}
	}
	return nil
}

func removePartial(ctx context.Context, remote string, obj fs.Object) {
	if obj == nil {
		return
	}
	if err := obj.Remove(ctx); err != nil && !errors.Is(err, fs.ErrorObjectNotFound) {
		fs.Debugf(nil, "Failed to remove incomplete %q: %v", remote, err)
	}
}

// destPath maps an archive entry name onto its destination remote within
// dstDir, returning an error if the name is unsafe.
//
// Archive entry names are attacker controlled so they are sanitized with
// sanitize.Path before being joined onto dstDir. This cleans the name
// (stripping the "./" prefix which tar archives created with relative
// paths, e.g. "tar -czf archive.tar.gz .", put on their entries) and
// rejects any name with a ".." path component which would otherwise
// escape the destination directory (a path traversal, or "Zip Slip",
// attack).
//
// The returned remote is empty for the archive root entry ("./"), which the
// caller should skip.
func destPath(nameInArchive, dstDir string) (string, error) {
	remote, err := sanitize.Path(nameInArchive)
	if err != nil {
		return "", fmt.Errorf("refusing to extract archive entry: %w", err)
	}
	if remote == "" {
		return "", nil
	}
	if dstDir != "" {
		remote = path.Join(dstDir, remote)
	}
	return remote, nil
}
