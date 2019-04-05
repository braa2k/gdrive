package drive

import (
	"fmt"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"io"
	"mime"
	"path/filepath"
	"time"
)

type UpdateArgs struct {
	Out         io.Writer
	Progress    io.Writer
	Id          string
	Path        string
	Name        string
	Description string
	Parents     []string
	Mime        string
	Recursive   bool
	ChunkSize   int64
	Timeout     time.Duration
}

func (self *Drive) Update(args UpdateArgs) error {
	// Instantiate empty drive file
	dstFile := &drive.File{Description: args.Description}
	call := self.service.Files.Update(args.Id, dstFile).Fields("id", "parents", "name", "size").SupportsTeamDrives(true)

    // Upload file if path is defined
	if args.Path != "" {
		srcFile, srcFileInfo, err := openFile(args.Path)
		if err != nil {
			return fmt.Errorf("Failed to open file: %s", err)
		}
		defer srcFile.Close()
		// Use provided file name or use filename
		if args.Name == "" {
			dstFile.Name = filepath.Base(srcFileInfo.Name())
		} else {
			dstFile.Name = args.Name
		}
		// Set provided mime type or get type based on file extension
		if args.Mime == "" {
			dstFile.MimeType = mime.TypeByExtension(filepath.Ext(dstFile.Name))
		} else {
			dstFile.MimeType = args.Mime
		}

		// Chunk size option
		chunkSize := googleapi.ChunkSize(int(args.ChunkSize))
		// Wrap file in progress reader
		progressReader := getProgressReader(srcFile, args.Progress, srcFileInfo.Size())
		// Wrap reader in timeout reader
		reader, ctx := getTimeoutReaderContext(progressReader, args.Timeout)
		fmt.Fprintf(args.Out, "Uploading %s\n", args.Path)
    	call.Context(ctx).Media(reader, chunkSize)	
	}	

	// Сhanging parent folders when set --parent
	if len(args.Parents) > 0 {
		// Set parent folders
		fileArgs := args
		previous_parent, err := self.service.Files.Get(args.Id).SupportsTeamDrives(true).Fields("parents").Do()
		if err != nil {
		    return fmt.Errorf("Failed to get file's parent: %s", err)
		}
		ParentsList := map[int][]string{}
		// Loop two times, first to find slice1 strings not in slice2,
		// second loop to find slice2 strings not in slice1
		for i := 0; i < 2; i++ {
			for _, s1 := range previous_parent.Parents {
				found := false
				for _, s2 := range fileArgs.Parents {
					if s1 == s2 {
						found = true
						break
					}
				}
				// String not found. We add it to return slice
				if !found {
					ParentsList[i] = append(ParentsList[i], s1)
				}
			}
			// Swap the slices, only if it was the first loop
			if i == 0 {
				previous_parent.Parents, fileArgs.Parents = fileArgs.Parents, previous_parent.Parents
			}
		} 
		if len(ParentsList[0]) > 0 || len(ParentsList[1]) > 0 {
			call.RemoveParents(formatList(ParentsList[0])).AddParents(formatList(ParentsList[1]))
		}
	}

	started := time.Now()
	f, err := call.Do()
	if err != nil {
		if isTimeoutError(err) {
			return fmt.Errorf("Failed to upload file: timeout, no data was transferred for %v", args.Timeout)
		}
		return fmt.Errorf("Failed to upload file: %s", err)
	}
	if len(args.Parents) > 0 {
		fmt.Fprintf(args.Out, "Move %s under new parent %s\n", f.Id, formatList(f.Parents))
	}
	if args.Path != "" {
		// Calculate average upload rate
		rate := calcRate(f.Size, started, time.Now())
		fmt.Fprintf(args.Out, "Updated %s at %s/s, total %s\n", f.Id, formatSize(rate, false), formatSize(f.Size, false))
	}
	return nil
}
