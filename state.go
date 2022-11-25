package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db/models"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	basePathDir    = "."
	basePathFormat = ".scout-2006-01-02"
	binExtension   = ".bin"
	dirPerms       = 0777
	filePerms      = 0666
)

// PathsToBytes acts as a lookup for paths to files on the disk to the bytes read from said files. This can represent both
// a read or a write process.
type PathsToBytes struct {
	inner map[string][]byte
	// BaseDir is the path to the directory that all paths within the PathsToBytes will be relative to. This can be an
	// absolute path or a relative path. All paths within the lookup will have this directory as a prefix.
	BaseDir string
}

// NewPathsToBytes creates a new PathsToBytes for the given directory.
func NewPathsToBytes(baseDir string) *PathsToBytes {
	return &PathsToBytes{
		inner:   make(map[string][]byte),
		BaseDir: baseDir,
	}
}

// LoadBaseDir will recursively load all the files within the BaseDir into the lookup.
func (ftb *PathsToBytes) LoadBaseDir() (err error) {
	if err = filepath.Walk(
		ftb.BaseDir,
		func(path string, info os.FileInfo, err error) error {
			log.INFO.Printf("PathsToBytes has visited %s (file = %t)", path, !info.IsDir())
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
		err = errors.Wrapf(err, "could not read directory \"%s\" into PathsToBytes", ftb.BaseDir)
	}
	return
}

// ReadPath will read the file at the given path into the lookup.
func (ftb *PathsToBytes) ReadPath(path string) (err error) {
	var data []byte
	if data, err = os.ReadFile(path); err != nil {
		err = errors.Wrapf(err, "cannot read file \"%s\" into PathsToBytes", path)
		return
	}
	ftb.AddPathBytes(path, data)
	return
}

// Path returns the BaseDir joined with the given filename, or in other words a path that is relative to the BaseDir.
func (ftb *PathsToBytes) Path(filename string) string {
	return filepath.Join(ftb.BaseDir, filename)
}

// AddPathBytes will create a lookup entry for bytes read from a given path. The path is relative to BaseDir.
func (ftb *PathsToBytes) AddPathBytes(path string, data []byte) {
	ftb.inner[path] = data
}

// AddFilenameBytes will create a lookup entry for bytes read from a given filename. This filename will first be normalised
// to a path that is relative to the BaseDir, and then AddPathBytes will be called.
func (ftb *PathsToBytes) AddFilenameBytes(filename string, data []byte) {
	ftb.AddPathBytes(ftb.Path(filename), data)
}

// Write will write all the paths in the lookup to the disk.
func (ftb *PathsToBytes) Write() (err error) {
	log.INFO.Printf("PathsToBytes is writing %d files to disk", len(ftb.inner))
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
			err = errors.Wrapf(err, "could not write bytes to file \"%s\" in PathsToBytes", filename)
			return
		}
	}
	return
}

// BytesForFilename looks up the given filename in the lookup, after converting the filename to a path relative to the
// BaseDir.
func (ftb *PathsToBytes) BytesForFilename(filename string) ([]byte, error) {
	filename = ftb.Path(filename)
	if data, ok := ftb.inner[filename]; ok {
		return data, nil
	} else {
		return data, fmt.Errorf("%s does not exist in PathsToBytes", filename)
	}
}

// BytesForDirectory lookups up files that have the given directory as a prefix. This is done after converting the
// directory name to a path relative to the BaseDir.
func (ftb *PathsToBytes) BytesForDirectory(directory string) *PathsToBytes {
	ftbOut := NewPathsToBytes(ftb.Path(directory))
	for path, data := range ftb.inner {
		if strings.HasPrefix(path, ftbOut.BaseDir) {
			ftbOut.AddPathBytes(path, data)
		}
	}
	return ftbOut
}

// Len returns the number of files in the lookup.
func (ftb *PathsToBytes) Len() int {
	return len(ftb.inner)
}

// CachedField represents a field in ScoutState that can be cached on-disk using the PathsToBytes lookup.
type CachedField interface {
	// BaseDir is the parent directory that the cached field will be stored in when serialised and written to disk.
	BaseDir() string
	// Path takes string arguments and returns the path that the cached field will be written to after being
	// serialised. This path should have the BaseDir as a prefix. The given arguments can be used within the path
	// returned by the procedure in any way that is necessary.
	Path(paths ...string) string
	// Serialise will serialise the CachedField instance to bytes and add the resulting files to the PathsToBytes lookup.
	// In this case PathsToBytes is write-only.
	Serialise(pathsToBytes *PathsToBytes) error
	// Deserialise will deserialise the bytes pertaining to this CachedField by looking up the necessary files from the
	// given PathsToBytes instance. In this case, PathsToBytes is read-only.
	Deserialise(pathsToBytes *PathsToBytes) error
	// SetOrAdd will set specific keys/fields in the CachedField to the given arguments. The given arguments can be used
	// in any way the user deems appropriate. For instance, for a cached field that is a map[string]any, the arguments
	// can be key-value pairs.
	SetOrAdd(args ...any)
	// Get will return the value at the given key/field. Like SetOrAdd this can be implemented in any way the user deems
	// appropriate.
	Get(key any) any
}

// IterableCachedField represents a field in ScoutState that can be cached, but also can be iterated through using the
// CachedFieldIterator.
type IterableCachedField interface {
	CachedField
	// Len returns the length of the iterable.
	Len() int
	// Iter returns the CachedFieldIterator that can be used to iterate over the instance of the CachedField itself.
	Iter() *CachedFieldIterator
	// Merge will perform a union/update procedure between the referred to IterableCachedField and the given
	// CachedField.
	Merge(field CachedField)
}

// CachedFieldIterator is the iterator pattern that is returned by the IterableCachedField.Iter method.
type CachedFieldIterator struct {
	cachedField IterableCachedField
	queue       []any
}

// Next checks whether the CachedFieldIterator has finished. I.e. there are no more elements to iterate over.
func (cfi *CachedFieldIterator) Next() bool {
	if len(cfi.queue) == 0 {
		return false
	}
	cfi.queue = cfi.queue[1:]
	return true
}

// Get will return the current element that is being iterated over.
func (cfi *CachedFieldIterator) Get() any {
	return cfi.cachedField.Get(cfi.queue[0])
}

// UserTweetTimes is used in the Scout procedure to store the times of the tweets for a Twitter user. The keys are
// comprised of Twitter user IDs. UserTweetTimes are serialised straight to JSON.
type UserTweetTimes map[string][]time.Time

func (utt *UserTweetTimes) BaseDir() string { return "" }

func (utt *UserTweetTimes) Path(paths ...string) string { return "userTweetTimes.json" }

func (utt *UserTweetTimes) Serialise(filenamesToBytes *PathsToBytes) (err error) {
	var data []byte
	if data, err = json.Marshal(&utt); err != nil {
		err = errors.Wrap(err, "cannot serialise UserTweetTimes")
		return
	}
	filenamesToBytes.AddFilenameBytes(utt.Path(), data)
	return
}

func (utt *UserTweetTimes) Deserialise(filenamesToBytes *PathsToBytes) (err error) {
	var data []byte
	if data, err = filenamesToBytes.BytesForFilename(utt.Path()); err != nil {
		err = errors.Wrap(err, "UserTweetTimes does not exist in cache")
		return
	}
	if err = json.Unmarshal(data, utt); err != nil {
		err = errors.Wrap(err, "could not deserialise UserTweetTimes")
		return
	}
	return
}

func (utt *UserTweetTimes) SetOrAdd(args ...any) {
	for argNo := 0; argNo < len(args); argNo += 2 {
		developerID := args[argNo].(string)
		tweetTime := args[argNo+1].(time.Time)
		if _, ok := (*utt)[developerID]; !ok {
			(*utt)[developerID] = make([]time.Time, 0)
		}
		(*utt)[developerID] = append((*utt)[developerID], tweetTime)
	}
}

func (utt *UserTweetTimes) Len() int { return len(*utt) }

func (utt *UserTweetTimes) Iter() *CachedFieldIterator {
	iter := &CachedFieldIterator{
		cachedField: utt,
		queue:       make([]any, utt.Len()),
	}
	i := 0
	for key := range *utt {
		iter.queue[i] = key
		i++
	}
	return iter
}

func (utt *UserTweetTimes) Get(key any) any {
	return (*utt)[key.(string)]
}

func (utt *UserTweetTimes) Merge(field CachedField) {
	for developerID, tweetTimes := range *field.(*UserTweetTimes) {
		if _, ok := (*utt)[developerID]; !ok {
			(*utt)[developerID] = make([]time.Time, len(tweetTimes))
			copy((*utt)[developerID], tweetTimes)
		} else {
			(*utt)[developerID] = append((*utt)[developerID], tweetTimes...)
		}
	}
}

// DeveloperSnapshots is used in the Scout procedure to store the partial models.DeveloperSnapshots that have been
// gathered for a specific Twitter user. Each of these lists of partial snapshots are then aggregated into a combined
// models.DeveloperSnapshot for each models.Developer.
//
// DeveloperSnapshots is serialised to a directory containing gob encoded files for each Twitter user containing the list
// of partial snapshots.
//
// The keys here are the IDs of the Twitter user that the snapshots apply to.
type DeveloperSnapshots map[string][]*models.DeveloperSnapshot

func (ds *DeveloperSnapshots) BaseDir() string { return "developerSnapshots" }

func (ds *DeveloperSnapshots) Path(paths ...string) string {
	return filepath.Join(ds.BaseDir(), strings.Join(paths, "_")+binExtension)
}

func (ds *DeveloperSnapshots) Serialise(filenamesToBytes *PathsToBytes) (err error) {
	for developerID, snapshots := range *ds {
		var data bytes.Buffer
		enc := gob.NewEncoder(&data)
		if err = enc.Encode(snapshots); err != nil {
			err = errors.Wrapf(err, "could not encode %d snapshots for developer \"%s\"", len(snapshots), developerID)
			return
		}
		filenamesToBytes.AddFilenameBytes(ds.Path(developerID), data.Bytes())
	}
	return
}

func (ds *DeveloperSnapshots) Deserialise(filenamesToBytes *PathsToBytes) (err error) {
	dsFtb := filenamesToBytes.BytesForDirectory(ds.BaseDir())
	for filename, data := range dsFtb.inner {
		developerID := strings.TrimSuffix(filename, binExtension)
		dec := gob.NewDecoder(bytes.NewReader(data))
		var snapshots []*models.DeveloperSnapshot
		if err = dec.Decode(&snapshots); err != nil {
			err = errors.Wrapf(err, "cannot deserialise snapshots for developer \"%s\"", developerID)
		}

		if _, ok := (*ds)[developerID]; !ok {
			(*ds)[developerID] = make([]*models.DeveloperSnapshot, len(snapshots))
			copy((*ds)[developerID], snapshots)
		} else {
			(*ds)[developerID] = append((*ds)[developerID], snapshots...)
		}
	}
	return
}

func (ds *DeveloperSnapshots) SetOrAdd(args ...any) {
	for argNo := 0; argNo < len(args); argNo += 2 {
		developerID := args[argNo].(string)
		snapshot := args[argNo+1].(*models.DeveloperSnapshot)
		if _, ok := (*ds)[developerID]; !ok {
			(*ds)[developerID] = make([]*models.DeveloperSnapshot, 0)
		}
		(*ds)[developerID] = append((*ds)[developerID], snapshot)
	}
}

func (ds *DeveloperSnapshots) Len() int {
	return len(*ds)
}

func (ds *DeveloperSnapshots) Iter() *CachedFieldIterator {
	iter := &CachedFieldIterator{
		cachedField: ds,
		queue:       make([]any, ds.Len()),
	}
	i := 0
	for key := range *ds {
		iter.queue[i] = key
		i++
	}
	return iter
}

func (ds *DeveloperSnapshots) Get(key any) any {
	return (*ds)[key.(string)]
}

func (ds *DeveloperSnapshots) Merge(field CachedField) {
	for developerID, snapshots := range *field.(*DeveloperSnapshots) {
		if _, ok := (*ds)[developerID]; !ok {
			(*ds)[developerID] = make([]*models.DeveloperSnapshot, len(snapshots))
			copy((*ds)[developerID], snapshots)
		} else {
			(*ds)[developerID] = append((*ds)[developerID], snapshots...)
		}
	}
}

// GameIDs is the set of all models.Game IDs that have been scraped/updated so far in the Scout procedure. GameIDs is
// serialised to a JSON array.
type GameIDs struct {
	mapset.Set[uuid.UUID]
}

func (ids *GameIDs) BaseDir() string { return "" }

func (ids *GameIDs) Path(paths ...string) string { return "gameIDs.json" }

func (ids *GameIDs) Serialise(filenamesToBytes *PathsToBytes) (err error) {
	var data []byte
	if data, err = json.Marshal(&ids); err != nil {
		err = errors.Wrap(err, "cannot serialise GameIDs")
		return
	}
	filenamesToBytes.AddFilenameBytes(ids.Path(), data)
	return
}

func (ids *GameIDs) Deserialise(filenamesToBytes *PathsToBytes) (err error) {
	var data []byte
	if data, err = filenamesToBytes.BytesForFilename(ids.Path()); err != nil {
		err = errors.Wrap(err, "GameIDs does not exist in cache")
		return
	}

	var uuids []uuid.UUID
	if err = json.Unmarshal(data, &uuids); err != nil {
		err = errors.Wrap(err, "could not deserialise GameIDs")
		return
	}
	ids.Set = mapset.NewSet[uuid.UUID](uuids...)
	return
}

func (ids *GameIDs) SetOrAdd(args ...any) {
	for _, arg := range args {
		ids.Set.Add(arg.(uuid.UUID))
	}
}

func (ids *GameIDs) Len() int {
	return ids.Cardinality()
}

func (ids *GameIDs) Iter() *CachedFieldIterator {
	cfi := &CachedFieldIterator{
		cachedField: ids,
		queue:       make([]any, ids.Len()),
	}

	i := 0
	iter := ids.Iterator()
	for val := range iter.C {
		cfi.queue[i] = val
		i++
	}
	return cfi
}

func (ids *GameIDs) Get(key any) any {
	return key
}

func (ids *GameIDs) Merge(field CachedField) {
	ids.Union(field.(*GameIDs).Set)
}

// Phase represents a phase in the Scout procedure.
type Phase int

const (
	Discovery Phase = iota
	Update
	Snapshot
	Disable
	Measure
)

// String returns the name of the Phase.
func (p Phase) String() string {
	switch p {
	case Discovery:
		return "Discovery"
	case Update:
		return "Update"
	case Snapshot:
		return "Snapshot"
	case Disable:
		return "Disable"
	case Measure:
		return "Measure"
	default:
		return "<nil>"
	}
}

// State is some additional information given to the Scout procedure that should also be cached. It is serialised
// straight to JSON.
type State struct {
	// Phase is the current phase that the Scout procedure is in. If ScoutState is loaded from disk this is the Phase
	// that the Scout procedure entered before stopping.
	Phase Phase
	// BatchSize is the size of the batches that should be used in a variety of different situations.
	BatchSize int
	// DiscoveryTweets is the total number of tweets that the DiscoveryPhase will fetch in batches of BatchSize.
	DiscoveryTweets int
}

func (s *State) BaseDir() string { return "" }

func (s *State) Path(paths ...string) string { return "state.json" }

func (s *State) Serialise(filenamesToBytes *PathsToBytes) (err error) {
	var data []byte
	if data, err = json.Marshal(s); err != nil {
		err = errors.Wrap(err, "cannot serialise State")
		return
	}
	filenamesToBytes.AddFilenameBytes(s.Path(), data)
	return
}

func (s *State) Deserialise(filenamesToBytes *PathsToBytes) (err error) {
	var data []byte
	if data, err = filenamesToBytes.BytesForFilename(s.Path()); err != nil {
		err = errors.Wrap(err, "State does not exist in cache")
		return
	}
	if err = json.Unmarshal(data, s); err != nil {
		err = errors.Wrap(err, "could not deserialise State")
		return
	}
	return
}

func (s *State) SetOrAdd(args ...any) {
	key := args[0].(string)
	switch key {
	case "Phase":
		s.Phase = args[1].(Phase)
	case "BatchSize":
		s.BatchSize = args[1].(int)
	case "DiscoveryTweets":
		s.DiscoveryTweets = args[1].(int)
	default:
		panic(fmt.Errorf("cannot set %s field of State to %v", key, args[1]))
	}
}

func (s *State) Get(key any) any {
	switch key {
	case "Phase":
		return s.Phase
	case "BatchSize":
		return s.BatchSize
	case "DiscoveryTweets":
		return s.DiscoveryTweets
	default:
		panic(fmt.Errorf("cannot get %s field of State", key))
	}
}

// CachedFieldType is an enum of all the fields in the ScoutState that are CachedField.
type CachedFieldType int

const (
	UserTweetTimesType CachedFieldType = iota
	DeveloperSnapshotsType
	GameIDsType
	StateType
)

// Make will return a new instance of the CachedField for the given CachedFieldType.
func (cft CachedFieldType) Make() CachedField {
	switch cft {
	case UserTweetTimesType:
		utt := make(UserTweetTimes)
		return &utt
	case DeveloperSnapshotsType:
		ds := make(DeveloperSnapshots)
		return &ds
	case GameIDsType:
		return &GameIDs{mapset.NewSet[uuid.UUID]()}
	case StateType:
		return &State{}
	default:
		return nil
	}
}

// String returns the name of the CachedFieldType.
func (cft CachedFieldType) String() string {
	switch cft {
	case UserTweetTimesType:
		return "UserTweetTimes"
	case DeveloperSnapshotsType:
		return "DeveloperSnapshots"
	case GameIDsType:
		return "GameIDs"
	case StateType:
		return "State"
	default:
		return "<nil>"
	}
}

// Types returns all the CachedFieldType in the enum.
func (cft CachedFieldType) Types() []CachedFieldType {
	return []CachedFieldType{
		UserTweetTimesType,
		DeveloperSnapshotsType,
		GameIDsType,
		StateType,
	}
}

// ScoutState represents the current state of the Scout procedure. It contains a number of CachedField that can be used
// to cache the ScoutState persistently on-disk. All CachedField are saved within a directory that contains the date on
// which the ScoutState was first created. The default behaviour for ScoutState is to load the directory with the latest
// date-stamp, this can be done using the StateLoadOrCreate procedure.
type ScoutState struct {
	createdTime  time.Time
	cachedFields map[CachedFieldType]CachedField
	mutex        sync.Mutex
}

func newState(forceCreate bool, createdTime time.Time) *ScoutState {
	if createdTime.IsZero() {
		createdTime = time.Now().UTC()
	}
	state := &ScoutState{
		createdTime:  createdTime,
		cachedFields: make(map[CachedFieldType]CachedField),
		mutex:        sync.Mutex{},
	}

	if forceCreate {
		log.INFO.Printf("Creating new cache: %s", state.BaseDir())
		// If the cache directory on disk already exists we will remove it and create a new one
		state.Delete()
	}

	// Then we allocate memory for all the fields that will be cached
	for _, cachedFieldType := range UserTweetTimesType.Types() {
		log.INFO.Printf("Creating cached field %s for %s", cachedFieldType.String(), state.BaseDir())
		state.cachedFields[cachedFieldType] = cachedFieldType.Make()
	}
	return state
}

// BaseDir returns the directory that this ScoutState will Save to.
func (state *ScoutState) BaseDir() string {
	return state.createdTime.Format(basePathFormat)
}

// GetCachedField will return the CachedField of the given CachedFieldType.
func (state *ScoutState) GetCachedField(fieldType CachedFieldType) CachedField {
	return state.cachedFields[fieldType]
}

// GetIterableCachedFields will return a mapping of all the CachedField that are IterableCachedField. The mapping will
// have keys that are CachedFieldType.
func (state *ScoutState) GetIterableCachedFields() map[CachedFieldType]IterableCachedField {
	iterableCachedFields := make(map[CachedFieldType]IterableCachedField)
	for cachedFieldType, cachedField := range state.cachedFields {
		if iterableCachedField, ok := cachedField.(IterableCachedField); ok {
			iterableCachedFields[cachedFieldType] = iterableCachedField
		}
	}
	return iterableCachedFields
}

// GetIterableCachedField will return the IterableCachedField of the given CachedFieldType.
func (state *ScoutState) GetIterableCachedField(fieldType CachedFieldType) IterableCachedField {
	return state.GetIterableCachedFields()[fieldType]
}

// Load will deserialise the relevant files in BaseDir into the CachedField within the ScoutState.
func (state *ScoutState) Load() (err error) {
	log.INFO.Printf("Loading ScoutState from %s", state.BaseDir())
	filenamesToBytes := NewPathsToBytes(state.BaseDir())
	if err = filenamesToBytes.LoadBaseDir(); err != nil {
		err = errors.Wrapf(err, "could not load ScoutState (%s) from disk", state.BaseDir())
	}

	for cachedFieldType, cacheField := range state.cachedFields {
		if err = cacheField.Deserialise(filenamesToBytes); err != nil {
			err = errors.Wrapf(err, "could not deserialise cached field %s", cachedFieldType.String())
		}
	}
	return
}

// Save will serialise all the CachedField to BaseDir, caching them persistently for later use.
func (state *ScoutState) Save() (err error) {
	// We remove the previous save from the disk
	if err = os.RemoveAll(state.BaseDir()); err != nil {
		err = errors.Wrapf(err, "could not remove \"%s\" before saving", state.BaseDir())
		return
	}

	filenamesToBytes := NewPathsToBytes(state.BaseDir())
	for cachedFieldType, cacheField := range state.cachedFields {
		switch cacheField.(type) {
		case IterableCachedField:
			log.INFO.Printf("Serialising iterable field %s which contains %d", cachedFieldType.String(), cacheField.(IterableCachedField).Len())
		default:
			log.INFO.Printf("Serialising non-iterable field %s which contains", cachedFieldType.String())
		}
		if err = cacheField.Serialise(filenamesToBytes); err != nil {
			err = errors.Wrapf(err, "could not serialise cached field %s", cachedFieldType.String())
		}
	}

	// Finally, we write the PathsToBytes to disk
	if err = filenamesToBytes.Write(); err != nil {
		err = errors.Wrap(err, "could not write serialised ScoutState to disk")
	}
	return
}

// Delete will delete the cache by deleting the BaseDir (if it exists).
func (state *ScoutState) Delete() {
	log.INFO.Printf("Deleting ScoutState cache %s", state.BaseDir())
	if _, err := os.Stat(state.BaseDir()); !os.IsNotExist(err) {
		_ = os.RemoveAll(state.BaseDir())
	} else {
		log.INFO.Printf("ScoutState cache %s does not exist", state.BaseDir())
	}
}

// StateLoadOrCreate will either create a new ScoutState for today, or load the latest ScoutState within the current
// directory. If forceCreate is set, then a new ScoutState will always be created. Be warned that if forceCreate is set
// then any existing cached ScoutState for the current day will be deleted.
func StateLoadOrCreate(forceCreate bool) (state *ScoutState, err error) {
	if forceCreate {
		state = newState(forceCreate, time.Now().UTC())
		return
	}

	// Find the newest cache in the basePathDir
	var files []os.DirEntry
	if files, err = os.ReadDir(basePathDir); err != nil {
		err = errors.Wrapf(err, "could not read base directory \"%s\"", basePathDir)
		return
	}

	log.INFO.Printf("Finding previous caches in \"%s\"", basePathDir)
	newestCacheTime := time.Time{}
	for _, f := range files {
		var cacheTime time.Time
		if cacheTime, err = time.Parse(basePathFormat, f.Name()); f.IsDir() && err == nil {
			log.INFO.Printf("Found previous cache \"%s\" for %s", f.Name(), cacheTime.String())
			if cacheTime.After(newestCacheTime) {
				newestCacheTime = cacheTime
			}
		}
	}

	// We default to always creating a cache. If the newest cache time IsZero then the current date will be taken
	// anyway.
	state = newState(forceCreate, newestCacheTime)
	if !newestCacheTime.IsZero() {
		// If we did find an old cache then we will load it
		err = state.Load()
	}
	return
}
