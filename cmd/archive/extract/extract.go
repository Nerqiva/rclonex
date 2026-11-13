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

	"github.com/bodgit/sevenzip"
	"github.com/mholt/archives"
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
	archivePassword   string
	archiveWindowSize int64
)

func init() {
	flagSet := Command.Flags()
	flags.StringVarP(flagSet, &archivePassword, "archive-password", "", "", "Password for encrypted RAR and 7z archives", "")
	flags.Int64VarP(flagSet, &archiveWindowSize, "archive-window-size", "", 16<<20, "Sliding window cache size in bytes for random-access archives (ZIP, 7z, ISO9660); 0 disables caching", "")
	archive.Command.AddCommand(Command)
}

// extractOptions carries the archive extraction command options to
// ArchiveExtract. It is stored in the context, mirroring how the filter
// config travels.
type extractOptions struct {
	password   string
	windowSize int64
}

const defaultWindowSize = 16 << 20

type extractOptionsKey struct{}

// withExtractOptions returns a context with the extract options set.
func withExtractOptions(ctx context.Context, o extractOptions) context.Context {
	return context.WithValue(ctx, extractOptionsKey{}, o)
}

// extractOptionsFrom extracts the options from the context, or returns the
// defaults if the context doesn't carry any - e.g. when ArchiveExtract is
// called directly.
func extractOptionsFrom(ctx context.Context) extractOptions {
	o, ok := ctx.Value(extractOptionsKey{}).(extractOptions)
	if !ok {
		o.windowSize = defaultWindowSize
	}
	return o
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
Formats which need random access (ZIP, 7z and ISO9660 images) are read
through a sliding window cache of ranged reads; formats which are single
pass (tar, tar.gz ...) are read sequentially. Remotes which do not
support range reads make ZIP, 7z and ISO9660 archives fail with an
explicit error rather than being accessed in some other way. ISO9660
images with Rock Ridge or Joliet extensions are supported, including
their long filenames.

Filters can be used to only extract certain files:

|||
$ rclonex archive extract archive.zip partial --include "bye.*"
$ rclonex tree partial
/
└── dir
    └── bye.txt
|||

RAR and 7z archives can be encrypted - use --archive-password to set
the password.

Multi-volume RAR and 7z archives (and encrypted multi-volume sets with
--archive-password) are supported - the volumes are streamed from the
remote in order, nothing is staged locally. Spanned/multi-disk ZIPs and
multi-part TAR archives are not supported.

The [archive backend](/archive/) can also be used to extract files. It
can be used to read only mount archives also but it supports a
different set of archive formats to the archive commands.
`, "|", "`"),

	Annotations: map[string]string{
		"versionIntroduced": "v1.72",
	},
	RunE: func(command *cobra.Command, args []string) error {
		cmd.CheckArgs(2, 2, command, args)

		opts := extractOptions{
			password:   archivePassword,
			windowSize: archiveWindowSize,
		}

		src, srcFile := cmd.NewFsFile(args[0])
		dst, dstFile := cmd.NewFsFile(args[1])

		cmd.Run(false, false, command, func() error {
			return ArchiveExtract(withExtractOptions(context.Background(), opts), dst, dstFile, src, srcFile)
		})
		return nil
	},
}

// ArchiveExtract extracts files from (src, srcFile) to (dst, dstDir)
func ArchiveExtract(ctx context.Context, dst fs.Fs, dstDir string, src fs.Fs, srcFile string) error {
	var srcObj fs.Object
	var filesExtracted = 0
	var err error

	fi := filter.GetConfig(ctx)
	ci := fs.GetConfig(ctx)
	opts := extractOptionsFrom(ctx)
	// get source object
	srcObj, err = src.NewObject(ctx, srcFile)
	fs.Debugf(nil, "srcFile: %q, src : %v", srcFile, src)
	if errors.Is(err, fs.ErrorIsDir) {
		return fmt.Errorf("source can't be a directory: %w", err)
	} else if errors.Is(err, fs.ErrorObjectNotFound) {
		return fmt.Errorf("source not found: %w", err)
	} else if err != nil {
		return fmt.Errorf("unable to access source: %w", err)
	}
	fs.Debugf(nil, "Source archive file: %s/%s", src.Root(), srcFile)
	// Create destination directory
	err = dst.Mkdir(ctx, dstDir)
	if err != nil {
		return fmt.Errorf("unable to access destination: %w", err)
	}

	fs.Debugf(dst, "Destination for extracted files: %q", dstDir)
	// start accounting
	tr := accounting.Stats(ctx).NewTransfer(srcObj, nil)
	defer tr.Done(ctx, err)
	// open source
	var options []fs.OpenOption
	for _, option := range fs.GetConfig(ctx).DownloadHeaders {
		options = append(options, option)
	}
	in0, err := operations.Open(ctx, srcObj, options...)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", srcFile, err)
	}
	defer func() {
		_ = in0.Close()
	}()
	// account and buffer the transfer
	// in = tr.Account(ctx, in).WithBuffer()
	acc := tr.Account(ctx, in0)
	in, err := acc.WithReadAtSeeker()
	if err != nil {
		return err
	}

	// handleEntry extracts one entry from the archive. It is shared by
	// the archive-format extraction path and the ISO9660 path. open opens
	// the entry for reading; it must not be nil for regular files.
	handleEntry := func(nameInArchive string, size int64, modTime time.Time, isDir bool, open func() (io.ReadCloser, error)) error {
		remote, err := destPath(nameInArchive, dstDir)
		if err != nil {
			return err
		}
		// Skip the archive root entry ("./") which has no name of its own.
		if remote == "" {
			return nil
		}
		// check if file should be extracted
		if !fi.Include(remote, size, modTime, fs.Metadata{}) {
			return nil
		}
		// process directory
		if isDir {
			// directory
			fs.Debugf(nil, "mkdir %s", remote)
			// leave if --dry-run set
			if ci.DryRun {
				return nil
			}
			// create the directory
			return operations.Mkdir(ctx, dst, remote)
		}
		// process file
		fs.Debugf(nil, "Extract %s", remote)
		// leave if --dry-run set
		if ci.DryRun {
			filesExtracted++
			return nil
		}
		// open file
		fin, err := open()
		if err != nil {
			return err
		}
		counter := &countingReadCloser{ReadCloser: fin}
		// extract the file to destination; Rcat creates its own per-file
		// transfer for the upload
		obj, err := operations.Rcat(ctx, dst, remote, counter, modTime, nil)
		if err == nil && size >= 0 && counter.n != size {
			// The upload succeeded but the archive didn't contain as many
			// bytes as it claims for the entry - don't keep the partial file.
			if removeErr := obj.Remove(ctx); removeErr != nil {
				fs.Debugf(nil, "Failed to remove incomplete %q: %v", remote, removeErr)
			}
			err = fmt.Errorf("archive entry %q: extracted %d bytes, expected %d (archive may be corrupt)", nameInArchive, counter.n, size)
		}
		if err == nil {
			filesExtracted++
		}
		return err
	}
	// ISO9660 images are not recognised by the archives library, so
	// detect them by the "CD001" standard identifier instead. Like ZIP
	// and 7z they are read through a windowed reader over ranged opens.
	if probeISO(in, srcObj.Size()) {
		if opts.password != "" {
			return fmt.Errorf("--archive-password is only supported for RAR and 7z archives")
		}
		// The probing stream is not used by the ISO path - release it
		// before the (potentially long) extraction
		_ = in0.Close()
		fs.Debugf(nil, "Extract %s/%s, format iso to %s", src.Root(), srcFile, dst.Root())
		tr.Reset(ctx)
		source, err := newWindowedReader(ctx, srcObj, opts.windowSize, tr, options...)
		if err != nil {
			return err
		}
		err = extractISO(ctx, source, srcObj.Size(), handleEntry)
		fs.Infof(nil, "Total files extracted %d", filesExtracted)
		return err
	}
	// identify format
	format, buffered, err := archives.Identify(ctx, "", in)
	if err != nil {
		return fmt.Errorf("failed to open check file type: %w", err)
	}
	fs.Debugf(nil, "Extract %s/%s, format %s to %s", src.Root(), srcFile, strings.TrimPrefix(format.Extension(), "."), dst.Root())

	// RAR opens its volumes by name from the remote through a small io/fs
	// adapter - the volumes are streamed from the remote, never staged
	// locally. Set Name and FS before the options are applied so they are
	// carried through onto the format used for extraction.
	if r, ok := format.(archives.Rar); ok {
		tr.Reset(ctx)
		r.Name = srcFile
		r.FS = remoteFS{ctx: ctx, f: src, acc: tr}
		format = r
	}

	// apply command options (password) to the format
	format, err = applyOptions(format, opts)
	if err != nil {
		return err
	}

	// check if extract is supported by format
	ex, isExtract := format.(archives.Extraction)
	if !isExtract {
		return fmt.Errorf("extraction for %s not supported", strings.TrimPrefix(format.Extension(), "."))
	}

	// Random access formats (ZIP and 7z) are read through a windowed
	// reader over ranged opens instead of the sequentially buffered
	// stream used to identify the format - re-opening the whole archive
	// per ReadAt would be far too slow. The buffered stream has served
	// its purpose; drop it so the windowed fetches can take over the
	// accounting for the archive transfer.
	var source io.Reader = buffered
	switch format.(type) {
	case archives.Zip:
		tr.Reset(ctx)
		source, err = newWindowedReader(ctx, srcObj, opts.windowSize, tr, options...)
		if err != nil {
			return err
		}
		// The probing stream is not used once the windowed reader takes over
		_ = in0.Close()
	case archives.SevenZip:
		tr.Reset(ctx)
		if strings.HasSuffix(strings.ToLower(srcFile), ".001") {
			volumes, totalSize, err := collectSevenZipVolumes(ctx, src, srcFile)
			if err != nil {
				return fmt.Errorf("failed to collect the 7z volume set for %q: %w", srcFile, err)
			}
			// The volume readers account the fetched bytes; the outer
			// reader must not account them a second time.
			source = newWindowedReaderFromFetch(ctx, totalSize, opts.windowSize, nil, concatFetch(ctx, volumes, tr, options...))
		} else {
			source, err = newWindowedReader(ctx, srcObj, opts.windowSize, tr, options...)
			if err != nil {
				return err
			}
		}
		// The probing stream is not used once the windowed reader takes over
		_ = in0.Close()
	case archives.Rar:
		// The stream is not used; the format reads its volumes from the
		// remote via the FS set above.
		source = nil
		_ = in0.Close()
	}

	// extract files
	err = ex.Extract(ctx, source, func(ctx context.Context, f archives.FileInfo) error {
		return handleEntry(f.NameInArchive, f.Size(), f.ModTime(), f.IsDir(), func() (io.ReadCloser, error) {
			return f.Open()
		})
	})
	err = encryptionHint(err, opts.password)

	fs.Infof(nil, "Total files extracted %d", filesExtracted)

	return err
}

// encryptionHint rewrites an error which indicates the archive is encrypted
// into a more helpful one. The extraction library reports a missing or wrong
// password as a low-level decrypt error, so this supplies the hint that
// --archive-password exists. It returns err unchanged otherwise.
func encryptionHint(err error, password string) error {
	var re *sevenzip.ReadError
	if !errors.As(err, &re) || !re.Encrypted {
		return err
	}
	if password == "" {
		return fmt.Errorf("archive is encrypted - use --archive-password to supply the password: %w", err)
	}
	return fmt.Errorf("failed to decrypt archive - the password supplied with --archive-password is incorrect (or the archive is corrupt): %w", err)
}

// applyOptions applies the password option to the
// identified format, returning the format to extract with.
//
// Formats wrapped in a CompressedArchive (e.g. tar.gz) get the option
// applied to the inner archive. The password is only meaningful to RAR and
// 7z archives, so an error is returned for other formats rather than
// silently ignoring it.
func applyOptions(format archives.Format, opts extractOptions) (archives.Format, error) {
	if ca, ok := format.(archives.CompressedArchive); ok {
		updated, err := applyExtractionOptions(ca.Extraction, opts)
		if err != nil {
			return nil, err
		}
		ca.Extraction = updated
		return ca, nil
	}
	ex, ok := format.(archives.Extraction)
	if !ok {
		if opts.password != "" {
			return nil, fmt.Errorf("--archive-password is only supported for RAR and 7z archives")
		}
		return format, nil
	}
	return applyExtractionOptions(ex, opts)
}

// applyExtractionOptions applies the password to a single extraction
// format. The password is rejected for every format other than RAR and 7z.
func applyExtractionOptions(ex archives.Extraction, opts extractOptions) (archives.Extraction, error) {
	switch f := ex.(type) {
	case archives.Zip:
		if opts.password != "" {
			return nil, fmt.Errorf("--archive-password is only supported for RAR and 7z archives")
		}
		return f, nil
	case archives.SevenZip:
		if opts.password != "" {
			f.Password = opts.password
		}
		return f, nil
	case archives.Rar:
		if opts.password != "" {
			f.Password = opts.password
		}
		return f, nil
	case archives.Tar:
		if opts.password != "" {
			return nil, fmt.Errorf("--archive-password is only supported for RAR and 7z archives")
		}
		return f, nil
	default:
		if opts.password != "" {
			return nil, fmt.Errorf("--archive-password is only supported for RAR and 7z archives")
		}
		return ex, nil
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
