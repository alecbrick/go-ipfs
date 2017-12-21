package mfs

import (
	"context"
	"fmt"
	"sync"

	chunk "github.com/ipfs/go-ipfs/importer/chunk"
	dag "github.com/ipfs/go-ipfs/merkledag"
	ft "github.com/ipfs/go-ipfs/unixfs"
	mod "github.com/ipfs/go-ipfs/unixfs/mod"

	node "gx/ipfs/QmNwUEK7QbwSqyKBu3mMtToo8SUc6wQJ7gdZq4gGGJqfnf/go-ipld-format"
)

type File struct {
	parent childCloser

	name string

	desclock sync.RWMutex

	dserv  dag.DAGService
	node   node.Node
	nodelk sync.RWMutex

	RawLeaves bool
}

// NewFile returns a NewFile object with the given parameters.  If the
// Cid version is non-zero RawLeaves will be enabled.
func NewFile(name string, node node.Node, parent childCloser, dserv dag.DAGService) (*File, error) {
	fi := &File{
		dserv:  dserv,
		parent: parent,
		name:   name,
		node:   node,
	}
	if node.Cid().Prefix().Version > 0 {
		fi.RawLeaves = true
	}
	return fi, nil
}

func (fi *File) Open(flags Flags) (_ FileDescriptor, _retErr error) {
	if flags.Write {
		fi.desclock.Lock()
		defer func() {
			if _retErr != nil {
				fi.desclock.Unlock()
			}
		}()
	} else if flags.Read {
		fi.desclock.RLock()
		defer func() {
			if _retErr != nil {
				fi.desclock.Unlock()
			}
		}()
	} else {
		return nil, fmt.Errorf("file opened for neither reading nor writing")
	}

	fi.nodelk.RLock()
	node := fi.node
	fi.nodelk.RUnlock()

	switch node := node.(type) {
	case *dag.ProtoNode:
		fsn, err := ft.FSNodeFromBytes(node.Data())
		if err != nil {
			return nil, err
		}

		switch fsn.Type {
		default:
			return nil, fmt.Errorf("unsupported fsnode type for 'file'")
		case ft.TSymlink:
			return nil, fmt.Errorf("symlinks not yet supported")
		case ft.TFile, ft.TRaw:
			// OK case
		}
	case *dag.RawNode:
		// Ok as well.
	}

	dmod, err := mod.NewDagModifier(context.TODO(), node, fi.dserv, chunk.DefaultSplitter)
	if err != nil {
		return nil, err
	}
	dmod.RawLeaves = fi.RawLeaves

	return &fileDescriptor{
		inode: fi,
		flags: flags,
		mod:   dmod,
	}, nil
}

// Size returns the size of this file
func (fi *File) Size() (int64, error) {
	fi.nodelk.RLock()
	defer fi.nodelk.RUnlock()
	switch nd := fi.node.(type) {
	case *dag.ProtoNode:
		pbd, err := ft.FromBytes(nd.Data())
		if err != nil {
			return 0, err
		}
		return int64(pbd.GetFilesize()), nil
	case *dag.RawNode:
		return int64(len(nd.RawData())), nil
	default:
		return 0, fmt.Errorf("unrecognized node type in mfs/file.Size()")
	}
}

// GetNode returns the dag node associated with this file
func (fi *File) GetNode() (node.Node, error) {
	fi.nodelk.RLock()
	defer fi.nodelk.RUnlock()
	return fi.node, nil
}

func (fi *File) Flush() error {
	// open the file in fullsync mode
	fd, err := fi.Open(Flags{Write: true, Sync: true})
	if err != nil {
		return err
	}

	defer fd.Close()

	return fd.Flush()
}

func (fi *File) Sync() error {
	// just being able to take the writelock means the descriptor is synced
	fi.desclock.Lock()
	fi.desclock.Unlock()
	return nil
}

// Type returns the type FSNode this is
func (fi *File) Type() NodeType {
	return TFile
}
