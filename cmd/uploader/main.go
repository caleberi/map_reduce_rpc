package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/rpc"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/caleberi/map_reduce_rpc/mrp"
)

const defaultChunkSize = 64 * 1024

func main() {
	coordinatorAddr := flag.String("coordinator", "localhost:1234", "Coordinator RPC address")
	folder := flag.String("folder", "", "Folder containing files to upload")
	chunkSize := flag.Int("chunk-size", defaultChunkSize, "Chunk size in bytes")
	recursive := flag.Bool("recursive", true, "Upload files recursively")
	flag.Parse()

	if strings.TrimSpace(*folder) == "" {
		log.Fatal("-folder is required")
	}
	if *chunkSize <= 0 {
		log.Fatal("-chunk-size must be > 0")
	}

	info, err := os.Stat(*folder)
	if err != nil {
		log.Fatalf("failed to stat folder %q: %v", *folder, err)
	}
	if !info.IsDir() {
		log.Fatalf("path %q is not a folder", *folder)
	}

	files, err := collectFiles(*folder, *recursive)
	if err != nil {
		log.Fatalf("failed collecting files: %v", err)
	}
	if len(files) == 0 {
		log.Printf("no files found in %s", *folder)
		return
	}

	client, err := rpc.Dial("tcp", *coordinatorAddr)
	if err != nil {
		log.Fatalf("failed to connect to coordinator %s: %v", *coordinatorAddr, err)
	}
	defer client.Close()

	uploaded := 0
	for _, path := range files {
		if err := uploadFile(client, path, *chunkSize); err != nil {
			log.Printf("upload failed for %s: %v", path, err)
			continue
		}
		uploaded++
		log.Printf("uploaded: %s", path)
	}

	log.Printf("upload complete: %d/%d files uploaded", uploaded, len(files))
}

func collectFiles(root string, recursive bool) ([]string, error) {
	files := make([]string, 0)

	if !recursive {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			files = append(files, filepath.Join(root, entry.Name()))
		}
		sort.Strings(files)
		return files, nil
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

func uploadFile(client *rpc.Client, path string, chunkSize int) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	var handleReply mrp.HandleReply
	if err := client.Call("Coordinator.RPCGenerateDownloadHandle", struct{}{}, &handleReply); err != nil {
		return fmt.Errorf("handle RPC failed: %w", err)
	}
	if handleReply.Status != "success" {
		return fmt.Errorf("handle generation failed: %s", handleReply.ErrorMessage)
	}

	buffer := make([]byte, chunkSize)
	for {
		n, readErr := file.Read(buffer)
		if readErr != nil && readErr != io.EOF {
			return readErr
		}

		isEOF := readErr == io.EOF
		if n == 0 && !isEOF {
			continue
		}

		request := mrp.DownloadRequest{
			Handle: handleReply.Handle,
			Data:   append([]byte(nil), buffer[:n]...),
			Eof:    isEOF,
		}
		var reply mrp.DownloadReply
		if err := client.Call("Coordinator.RPCForwardDownload", request, &reply); err != nil {
			return fmt.Errorf("forward RPC failed: %w", err)
		}
		if reply.Status != "success" {
			return fmt.Errorf("forward failed: %s (%d)", reply.ErrorMessage, reply.ErrorCode)
		}

		if isEOF {
			break
		}
	}

	return nil
}
