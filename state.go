package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db/models"
	myErrors "github.com/andygello555/game-scout/errors"
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/andygello555/gotils/v2/slices"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"os"
	"path/filepath"
	"reflect"
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
	start := time.Now().UTC()
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
	log.INFO.Printf(
		"PathsToBytes has finished writing %d files to disk in %s",
		len(ftb.inner), time.Now().UTC().Sub(start).String(),
	)
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
	// In this case PathsToBytes is write-only. If the instance is going to be serialised to a directory structure then
	// remember to add placeholder/metadata files in the root of each directory so that the directories are created by
	// PathsToBytes and the structure can be deserialised.
	Serialise(pathsToBytes *PathsToBytes) error
	// Deserialise will deserialise the bytes pertaining to this CachedField by looking up the necessary files from the
	// given PathsToBytes instance. In this case, PathsToBytes is read-only.
	Deserialise(pathsToBytes *PathsToBytes) error
	// SetOrAdd will set specific keys/fields in the CachedField to the given arguments. The given arguments can be used
	// in any way the user deems appropriate. For instance, for a cached field that is a map[string]any, the arguments
	// can be key-value pairs.
	SetOrAdd(args ...any)
	// Get will return the value at the given key/field. The second result value determines whether the key/field exists
	// in the implemented instance. Like SetOrAdd this can be implemented in any way the user deems appropriate.
	Get(key any) (any, bool)
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
	i           int
	cachedField IterableCachedField
	queue       []any
}

// Continue checks whether the CachedFieldIterator has not finished. I.e. there are still more elements to iterate over.
func (cfi *CachedFieldIterator) Continue() bool {
	return len(cfi.queue) != 0
}

// I will return the current i value.
func (cfi *CachedFieldIterator) I() int {
	return cfi.i
}

// Key will return the current key of the element that is being iterated over.
func (cfi *CachedFieldIterator) Key() any {
	return cfi.queue[0]
}

// Get will return the current element that is being iterated over.
func (cfi *CachedFieldIterator) Get() (any, bool) {
	return cfi.cachedField.Get(cfi.Key())
}

// Next will dequeue the current element. This should be called at the end of each loop.
func (cfi *CachedFieldIterator) Next() {
	cfi.i++
	cfi.queue = cfi.queue[1:]
}

// UserTweetTimes is used in the Scout procedure to store the times of the tweets for a Twitter user. The keys are
// comprised of Twitter user IDs. UserTweetTimes are serialised straight to JSON.
type UserTweetTimes map[string][]time.Time

func (utt *UserTweetTimes) BaseDir() string { return "" }

func (utt *UserTweetTimes) Path(paths ...string) string { return "userTweetTimes.json" }

func (utt *UserTweetTimes) Serialise(pathsToBytes *PathsToBytes) (err error) {
	var data []byte
	if data, err = json.Marshal(&utt); err != nil {
		err = errors.Wrap(err, "cannot serialise UserTweetTimes")
		return
	}
	pathsToBytes.AddFilenameBytes(utt.Path(), data)
	return
}

func (utt *UserTweetTimes) Deserialise(pathsToBytes *PathsToBytes) (err error) {
	var data []byte
	if data, err = pathsToBytes.BytesForFilename(utt.Path()); err != nil {
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

func (utt *UserTweetTimes) Get(key any) (any, bool) {
	return (*utt)[key.(string)], false
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
// DeveloperSnapshots is serialised to a directory containing a meta.json file and gob encoded files for each Twitter user
// containing the list of partial snapshots.
//
// The keys here are the IDs of the Twitter user that the snapshots apply to.
type DeveloperSnapshots map[string][]*models.DeveloperSnapshot

func (ds *DeveloperSnapshots) BaseDir() string { return "developerSnapshots" }

func (ds *DeveloperSnapshots) Path(paths ...string) string {
	return filepath.Join(ds.BaseDir(), strings.Join(paths, "_")+binExtension)
}

func (ds *DeveloperSnapshots) Serialise(pathsToBytes *PathsToBytes) (err error) {
	// We create an info file so that BaseDir is always created
	pathsToBytes.AddFilenameBytes(
		filepath.Join(ds.BaseDir(), "meta.json"),
		[]byte(fmt.Sprintf(
			"{\"count\": %d, \"ids\": [%s]}",
			ds.Len(),
			strings.Join(strings.Split(strings.Trim(fmt.Sprintf("%v", ds.Iter().queue), "[]"), " "), ", "),
		)),
	)
	for developerID, snapshots := range *ds {
		var data bytes.Buffer
		enc := gob.NewEncoder(&data)
		if err = enc.Encode(snapshots); err != nil {
			err = errors.Wrapf(err, "could not encode %d snapshots for developer \"%s\"", len(snapshots), developerID)
			return
		}
		pathsToBytes.AddFilenameBytes(ds.Path(developerID), data.Bytes())
	}
	return
}

func (ds *DeveloperSnapshots) Deserialise(pathsToBytes *PathsToBytes) (err error) {
	dsFtb := pathsToBytes.BytesForDirectory(ds.BaseDir())
	for filename, data := range dsFtb.inner {
		if filepath.Base(filename) != "meta.json" {
			// Trim the PathToBytes BaseDir, the DeveloperSnapshots BaseDir, and the file extension
			developerID := strings.TrimSuffix(filepath.Base(filename), binExtension)
			dec := gob.NewDecoder(bytes.NewReader(data))
			var snapshots []*models.DeveloperSnapshot
			if err = dec.Decode(&snapshots); err != nil {
				err = errors.Wrapf(err, "cannot deserialise snapshots for developer \"%s\"", developerID)
				return
			}

			if _, ok := (*ds)[developerID]; !ok {
				(*ds)[developerID] = make([]*models.DeveloperSnapshot, len(snapshots))
				copy((*ds)[developerID], snapshots)
			} else {
				(*ds)[developerID] = append((*ds)[developerID], snapshots...)
			}
		}
	}
	dsFtb = nil
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

func (ds *DeveloperSnapshots) Get(key any) (any, bool) {
	return (*ds)[key.(string)], false
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

func (ids *GameIDs) Serialise(pathsToBytes *PathsToBytes) (err error) {
	var data []byte
	if data, err = json.Marshal(&ids); err != nil {
		err = errors.Wrap(err, "cannot serialise GameIDs")
		return
	}
	pathsToBytes.AddFilenameBytes(ids.Path(), data)
	return
}

func (ids *GameIDs) Deserialise(pathsToBytes *PathsToBytes) (err error) {
	var data []byte
	if data, err = pathsToBytes.BytesForFilename(ids.Path()); err != nil {
		err = errors.Wrap(err, "GameIDs does not exist in cache")
		return
	}

	var uuids []uuid.UUID
	if err = json.Unmarshal(data, &uuids); err != nil {
		err = errors.Wrap(err, "could not deserialise GameIDs")
		return
	}
	ids.Set = mapset.NewThreadUnsafeSet[uuid.UUID](uuids...)
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

func (ids *GameIDs) Get(key any) (any, bool) {
	return key, ids.Contains(key.(uuid.UUID))
}

func (ids *GameIDs) Merge(field CachedField) {
	ids.Set = ids.Union(field.(*GameIDs).Set)
}

type DeletedDevelopers []*models.TrendingDev

func (d *DeletedDevelopers) BaseDir() string { return "deletedDevelopers" }

func (d *DeletedDevelopers) Path(paths ...string) string {
	return filepath.Join(d.BaseDir(), strings.Join(paths, "_")+binExtension)
}

func (d *DeletedDevelopers) Serialise(pathsToBytes *PathsToBytes) (err error) {
	// We create an info file so that BaseDir is always created
	pathsToBytes.AddFilenameBytes(
		filepath.Join(d.BaseDir(), "meta.json"),
		[]byte(fmt.Sprintf(
			"{\"count\": %d, \"ids\": [%s]}",
			d.Len(),
			strings.Join(strings.Split(strings.Trim(fmt.Sprintf(
				"%v",
				slices.Comprehension(*d, func(idx int, value *models.TrendingDev, arr []*models.TrendingDev) string {
					return value.Developer.ID
				})), "[]"), " "), ", ",
			),
		)),
	)

	for _, deletedDeveloper := range *d {
		developer := deletedDeveloper.Developer
		for _, field := range []struct {
			value any
			name  string
		}{
			{
				deletedDeveloper.Developer,
				"developer",
			},
			{
				deletedDeveloper.Snapshots,
				"snapshots",
			},
			{
				deletedDeveloper.Games,
				"games",
			},
			{
				deletedDeveloper.Trend,
				"trend",
			},
		} {
			var data bytes.Buffer
			enc := gob.NewEncoder(&data)
			if err = enc.Encode(field.value); err != nil {
				err = errors.Wrapf(err, "could not encode %s for deleted Developer %v", field.name, developer)
				return
			}
			pathsToBytes.AddFilenameBytes(d.Path(developer.ID, field.name), data.Bytes())
		}
	}
	return
}

func deletedDevelopersDecodeValue[T interface {
	models.Developer | []*models.DeveloperSnapshot | []*models.Game | models.Trend
}](data []byte, fieldName, developerID string) (decodedValue *T, err error) {
	decodedValue = new(T)
	dec := gob.NewDecoder(bytes.NewReader(data))
	if err = dec.Decode(decodedValue); err != nil {
		err = errors.Wrapf(err, "cannot deserialise %s for deleted developer \"%s\"", fieldName, developerID)
	}
	return
}

func (d *DeletedDevelopers) Deserialise(pathsToBytes *PathsToBytes) (err error) {
	dFtb := pathsToBytes.BytesForDirectory(d.BaseDir())
	deletedDeveloperMap := make(map[string]*models.TrendingDev)
	for filename, data := range dFtb.inner {
		base := filepath.Base(filename)
		if base != "meta.json" {
			// Trim the PathToBytes BaseDir, the DeletedDevelopers BaseDir, and the file extension
			fileComponents := strings.Split(strings.TrimSuffix(base, binExtension), "_")
			developerID, fieldName := fileComponents[0], fileComponents[1]

			if _, ok := deletedDeveloperMap[developerID]; !ok {
				deletedDeveloperMap[developerID] = &models.TrendingDev{}
			}
			deletedDeveloper := deletedDeveloperMap[developerID]

			// Decode the value using the appropriate type parameter for the field name found in the filename
			switch fieldName {
			case "developer":
				if deletedDeveloper.Developer, err = deletedDevelopersDecodeValue[models.Developer](data, fieldName, developerID); err != nil {
					return
				}
			case "snapshots":
				var snapshots *[]*models.DeveloperSnapshot
				if snapshots, err = deletedDevelopersDecodeValue[[]*models.DeveloperSnapshot](data, fieldName, developerID); err != nil {
					return
				}
				deletedDeveloper.Snapshots = *snapshots
			case "games":
				var games *[]*models.Game
				if games, err = deletedDevelopersDecodeValue[[]*models.Game](data, fieldName, developerID); err != nil {
					return
				}
				deletedDeveloper.Games = *games
			case "trend":
				if deletedDeveloper.Trend, err = deletedDevelopersDecodeValue[models.Trend](data, fieldName, developerID); err != nil {
					return
				}
			default:
				return
			}
		}
	}

	// Go through all the DeletedDevelopers in the map and insert them into the referred to DeletedDevelopers instance
	for _, deletedDeveloper := range deletedDeveloperMap {
		*d = append(*d, deletedDeveloper)
	}
	dFtb = nil
	return
}

func (d *DeletedDevelopers) SetOrAdd(args ...any) {
	// DeletedDevelopers should act as a set, so we will check if each arg doesn't already exist in DeletedDevelopers
	for _, arg := range args {
		found := false
		toBeDeletedDeveloper := arg.(*models.TrendingDev)
		for _, deletedDeveloper := range *d {
			if deletedDeveloper.Developer.ID == toBeDeletedDeveloper.Developer.ID {
				found = true
				// If Games, Snapshots, or Trend is not set in the currently existing reference to the deleted developer,
				// but they do exist in the deleted developer that is being added. We will set those fields.
				if deletedDeveloper.Games == nil && toBeDeletedDeveloper.Games != nil {
					deletedDeveloper.Games = toBeDeletedDeveloper.Games
				}

				if deletedDeveloper.Snapshots == nil && toBeDeletedDeveloper.Snapshots != nil {
					deletedDeveloper.Snapshots = toBeDeletedDeveloper.Snapshots
				}

				if deletedDeveloper.Trend == nil && toBeDeletedDeveloper.Trend != nil {
					deletedDeveloper.Trend = toBeDeletedDeveloper.Trend
				}
				break
			}
		}

		if !found {
			*d = append(*d, arg.(*models.TrendingDev))
		}
	}
}

func (d *DeletedDevelopers) Get(key any) (any, bool) {
	switch key.(type) {
	case int:
		// Get the key-th DeletedDeveloper
		i := key.(int)
		if i < 0 || i > d.Len() {
			return nil, false
		}
		return (*d)[key.(int)], true
	case string:
		// Get the DeletedDeveloper by ID
		var value any = nil
		ok := false
		for _, deletedDeveloper := range *d {
			if deletedDeveloper.Developer.ID == key.(string) {
				value = deletedDeveloper
				ok = true
				break
			}
		}
		return value, ok
	default:
		return nil, false
	}
}

func (d *DeletedDevelopers) Len() int {
	return len(*d)
}

func (d *DeletedDevelopers) Iter() *CachedFieldIterator {
	cfi := &CachedFieldIterator{
		cachedField: d,
		queue:       make([]any, d.Len()),
	}

	for i, deletedDeveloper := range *d {
		cfi.queue[i] = deletedDeveloper
	}
	return cfi
}

func (d *DeletedDevelopers) Merge(field CachedField) {
	dNew := make(DeletedDevelopers, d.Len()+field.(*DeletedDevelopers).Len())
	copy(dNew, *d)
	copy(dNew[d.Len():], *field.(*DeletedDevelopers))
	*d = dNew
}

// Phase represents a phase in the Scout procedure.
type Phase int

const (
	Discovery Phase = iota
	Update
	Snapshot
	Disable
	Enable
	Delete
	Measure
	Done
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
	case Enable:
		return "Enable"
	case Delete:
		return "Delete"
	case Measure:
		return "Measure"
	case Done:
		return "Done"
	default:
		return "<nil>"
	}
}

func (p Phase) Run(state *ScoutState) (err error) {
	var phaseStart time.Time
	phaseBefore := func() {
		phaseStart = time.Now().UTC()
		state.GetCachedField(StateType).SetOrAdd("PhaseStart", phaseStart)
		phase, _ := state.GetCachedField(StateType).Get("Phase")
		log.INFO.Printf("Starting %s phase", phase.(Phase).String())
	}

	phaseAfter := func() {
		phase, _ := state.GetCachedField(StateType).Get("Phase")
		log.INFO.Printf("Finished %s phase in %s", phase.(Phase).String(), time.Now().UTC().Sub(phaseStart).String())
		nextPhase := phase.(Phase).Next()
		state.GetCachedField(StateType).SetOrAdd("Phase", nextPhase)
		if err = state.Save(); err != nil {
			log.ERROR.Printf("Could not save ScoutState: %v", err)
		}
	}

	phase, _ := state.GetCachedField(StateType).Get("Phase")
	if phase.(Phase) != p {
		return fmt.Errorf(
			"state's saved phase (%s) does not much the phase being run (%s)",
			phase.(Phase).String(), p.String(),
		)
	}

	switch phase.(Phase) {
	case Discovery:
		discoveryTweetsAny, _ := state.GetCachedField(StateType).Get("DiscoveryTweets")
		discoveryTweets := discoveryTweetsAny.(int)

		// If the number of discovery tweets we requested is more than maxDiscoveryTweetsDailyPercent of the total tweets
		// available for a day then we will error out.
		if float64(discoveryTweets) > maxTotalDiscoveryTweets {
			return myErrors.TemporaryErrorf(
				false,
				"cannot request more than %d tweets per day for discovery purposes",
				int(myTwitter.TweetsPerDay),
			)
		}

		phaseBefore()

		// 1st phase: Discovery. Search the recent tweets on Twitter belonging to the hashtags given in config.json.
		var subGameIDs mapset.Set[uuid.UUID]
		if subGameIDs, err = DiscoveryPhase(state); err != nil {
			return err
		}
		state.GetIterableCachedField(GameIDsType).Merge(&GameIDs{subGameIDs})

		phaseAfter()
	case Update:
		// 2nd phase: Update. For any developers, and games that exist in the DB but haven't been scraped we will fetch the
		// details from the DB and initiate scrapes for them
		log.INFO.Printf("We have scraped:")
		log.INFO.Printf("\t%d developers", state.GetIterableCachedField(DeveloperSnapshotsType).Len())
		log.INFO.Printf("\t%d games", state.GetIterableCachedField(DeveloperSnapshotsType).Len())

		phaseBefore()

		// Extract the IDs of the developers that were just scraped in the discovery phase.
		scrapedDevelopers := make([]string, state.GetIterableCachedField(DeveloperSnapshotsType).Len())
		iter := state.GetIterableCachedField(DeveloperSnapshotsType).Iter()
		for iter.Continue() {
			scrapedDevelopers[iter.I()] = iter.Key().(string)
			iter.Next()
		}

		// If there are already developers that have been updated in a previously run UpdatePhase, then we will add
		// these to the scrapedDevelopers also
		stateState := state.GetCachedField(StateType).(*State)
		if len(stateState.UpdatedDevelopers) > 0 {
			scrapedDevelopers = append(scrapedDevelopers, stateState.UpdatedDevelopers...)
		}

		// Run the UpdatePhase.
		if err = UpdatePhase(scrapedDevelopers, state); err != nil {
			return err
		}

		phaseAfter()
	case Snapshot:
		// 3rd phase: Snapshot. For all the partial DeveloperSnapshots that exist in the developerSnapshots map we will
		// aggregate the information for them.
		phaseBefore()

		if err = SnapshotPhase(state); err != nil {
			return errors.Wrap(err, "snapshot phase has failed")
		}

		phaseAfter()
	case Disable:
		// 4th phase: Disable. Disable the developers who aren't doing so well. The number of developers to disable depends
		// on the number of tweets we get to use in the update phase the next time around.
		phaseBefore()

		if err = DisablePhase(state); err != nil {
			return errors.Wrap(err, "disable phase has failed")
		}

		phaseAfter()
	case Enable:
		// 5th phase: Enable: Enable a certain amount of developers who were disabled before today based off of how well
		// their snapshots have been trending.
		phaseBefore()

		if err = EnablePhase(state); err != nil {
			return errors.Wrap(err, "enable phase has failed")
		}

		phaseAfter()
	case Delete:
		// 6th phase: Delete. Delete a certain percentage of the disabled developer that are performing the worst. This
		// is decided by calculating their trend so far.
		phaseBefore()

		if err = DeletePhase(state); err != nil {
			return errors.Wrap(err, "delete phase has failed")
		}

		phaseAfter()
	case Measure:
		// 7th phase: Measure. For all the users that have a good number of snapshots we'll see if any are shining above
		// the rest
		phaseBefore()

		if err = MeasurePhase(state); err != nil {
			return errors.Wrap(err, "measure phase has failed")
		}

		phaseAfter()
	default:
		// This will be Done which we don't need to do anything for.
	}
	return
}

// PhaseFromString returns the Phase that has the given name. If there is no Phase of that name, then Discovery will be
// returned instead. Case-insensitive.
func PhaseFromString(s string) Phase {
	switch strings.ToLower(s) {
	case "discovery":
		return Discovery
	case "update":
		return Update
	case "snapshot":
		return Snapshot
	case "disable":
		return Disable
	case "enable":
		return Enable
	case "delete":
		return Delete
	case "measure":
		return Measure
	case "done":
		return Done
	default:
		return Discovery
	}
}

// Next returns the Phase after this one. If the referred to Phase is the last phase (Done) then the last Phase will be
// returned.
func (p Phase) Next() Phase {
	if p == Done {
		return p
	}
	return p + 1
}

// State is some additional information given to the Scout procedure that should also be cached. It is serialised
// straight to JSON.
type State struct {
	// Phase is the current phase that the Scout procedure is in. If ScoutState is loaded from disk this is the Phase
	// that the Scout procedure entered before stopping.
	Phase Phase
	// Result is the ScoutResult that will be saved once the Scout procedure reaches the Done Phase.
	Result *models.ScoutResult
	// BatchSize is the size of the batches that should be used in a variety of different situations.
	BatchSize int
	// DiscoveryTweets is the total number of tweets that the DiscoveryPhase will fetch in batches of BatchSize.
	DiscoveryTweets int
	// CurrentDiscoveryBatch is the current batch that the DiscoveryPhase is on. Of course, this won't apply to any
	// other Phase other than the Discovery phase.
	CurrentDiscoveryBatch int
	// CurrentDiscoveryToken is the token for the Twitter RecentSearch API that the DiscoveryPhase is on. Of course,
	// this won't apply to any other Phase other than the Discovery phase.
	CurrentDiscoveryToken string
	// UpdatedDevelopers are the developer IDs that have been updated in a previous UpdatePhase. This applies to both
	// the Discovery and Update Phase.
	UpdatedDevelopers []string
	// DisabledDevelopers are the IDs of the models.Developer that have been disabled in a previous DisablePhase. This
	// applies to both the Disable and Enable Phase.
	DisabledDevelopers []string
	// EnabledDevelopers are the IDs of the models.Developer that were re-enabled in a previous EnablePhase. This
	// applies to only the Enable Phase.
	EnabledDevelopers []string
	// DevelopersToEnable is the number of developers to go to be re-enabled in the EnablePhase. This applies only to
	// the Enable Phase.
	DevelopersToEnable int
	// PhaseStart is the time that the most recent Phase was started.
	PhaseStart time.Time
	// Start time of the Scout procedure.
	Start time.Time
	// Finished is when the Scout procedure finished. This should technically never be set for long as the ScoutState
	// should be deleted straight after the Scout procedure has completed.
	Finished time.Time
	// Debug indicates whether the process is in debug mode. This has various effects throughout the scout process, such
	// as not incrementing the TimesHighlighted field of Developers and SteamApps in the Measure Phase.
	Debug bool
}

func (s *State) BaseDir() string { return "" }

func (s *State) Path(paths ...string) string { return "state.json" }

func (s *State) Serialise(pathsToBytes *PathsToBytes) (err error) {
	var data []byte
	if data, err = json.Marshal(s); err != nil {
		err = errors.Wrap(err, "cannot serialise State")
		return
	}
	pathsToBytes.AddFilenameBytes(s.Path(), data)
	return
}

func (s *State) Deserialise(pathsToBytes *PathsToBytes) (err error) {
	var data []byte
	if data, err = pathsToBytes.BytesForFilename(s.Path()); err != nil {
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
	case "Result":
		path := slices.Comprehension(args[1:len(args)-1], func(idx int, value any, arr []any) string { return value.(string) })
		currentVal := reflect.ValueOf(s.Result)
		for i, pathKey := range path {
			currentVal = currentVal.Elem().FieldByName(pathKey)
			if i == len(path)-1 {
				setVal := reflect.ValueOf(args[len(args)-1])
				if setVal.Type().Kind() == reflect.Func {
					returnVars := setVal.Call([]reflect.Value{currentVal})
					currentVal.Set(returnVars[0])
				} else {
					currentVal.Set(setVal)
				}
			}
		}
	case "BatchSize":
		s.BatchSize = args[1].(int)
	case "DiscoveryTweets":
		s.DiscoveryTweets = args[1].(int)
	case "CurrentDiscoveryBatch":
		s.CurrentDiscoveryBatch = args[1].(int)
	case "CurrentDiscoveryToken":
		s.CurrentDiscoveryToken = args[1].(string)
	case "UpdatedDevelopers":
		// We will append to UpdatedDevelopers rather than set it
		s.UpdatedDevelopers = append(s.UpdatedDevelopers, args[1].(string))
	case "DisabledDevelopers":
		s.DisabledDevelopers = args[1].([]string)
	case "EnabledDevelopers":
		s.EnabledDevelopers = append(s.EnabledDevelopers, args[1].(string))
	case "DevelopersToEnable":
		s.DevelopersToEnable = args[1].(int)
	case "PhaseStart":
		s.PhaseStart = args[1].(time.Time)
	case "Start":
		s.Start = args[1].(time.Time)
	case "Finished":
		s.Finished = args[1].(time.Time)
	case "Debug":
		s.Debug = args[1].(bool)
	default:
		panic(fmt.Errorf("cannot set %s field of State to %v", key, args[1]))
	}
}

func (s *State) Get(key any) (any, bool) {
	switch key {
	case "Phase":
		return s.Phase, true
	case "Result":
		return s.Result, true
	case "BatchSize":
		return s.BatchSize, true
	case "DiscoveryTweets":
		return s.DiscoveryTweets, true
	case "CurrentDiscoveryBatch":
		return s.CurrentDiscoveryBatch, true
	case "CurrentDiscoveryToken":
		return s.CurrentDiscoveryToken, true
	case "UpdatedDevelopers":
		return s.UpdatedDevelopers, true
	case "DisabledDevelopers":
		return s.DisabledDevelopers, true
	case "EnabledDevelopers":
		return s.EnabledDevelopers, true
	case "DevelopersToEnable":
		return s.DevelopersToEnable, true
	case "PhaseStart":
		return s.PhaseStart, true
	case "Start":
		return s.Start, true
	case "Finished":
		return s.Finished, true
	case "Debug":
		return s.Debug, true
	default:
		return nil, false
	}
}

// CachedFieldType is an enum of all the fields in the ScoutState that are CachedField.
type CachedFieldType int

const (
	UserTweetTimesType CachedFieldType = iota
	DeveloperSnapshotsType
	GameIDsType
	DeletedDevelopersType
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
		return &GameIDs{mapset.NewThreadUnsafeSet[uuid.UUID]()}
	case DeletedDevelopersType:
		dd := make(DeletedDevelopers, 0)
		return &dd
	case StateType:
		return &State{
			Result: &models.ScoutResult{
				DiscoveryStats: &models.DiscoveryUpdateSnapshotStats{},
				UpdateStats:    &models.DiscoveryUpdateSnapshotStats{},
				SnapshotStats:  &models.DiscoveryUpdateSnapshotStats{},
				DisableStats:   &models.DisableEnableDeleteStats{},
				EnableStats:    &models.DisableEnableDeleteStats{},
				DeleteStats:    &models.DisableEnableDeleteStats{},
				MeasureStats:   &models.MeasureStats{},
			},
			UpdatedDevelopers:  make([]string, 0),
			DisabledDevelopers: make([]string, 0),
			EnabledDevelopers:  make([]string, 0),
		}
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
	case DeletedDevelopersType:
		return "DeletedDevelopers"
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
		DeletedDevelopersType,
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
	// Whether ScoutState was loaded from a previously cached ScoutState.
	Loaded bool
	// Whether the ScoutState only exists in memory. Useful for a temporary ScoutState that will be merged back into a
	// main ScoutState.
	InMemory bool
	mutex    sync.Mutex
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
	state.makeCachedFields()
	return state
}

// makeCachedFields will call CachedFieldType.Make on all CachedFieldType and store the made CachedField in
// ScoutState.cachedFields.
func (state *ScoutState) makeCachedFields() {
	for _, cachedFieldType := range UserTweetTimesType.Types() {
		log.INFO.Printf("Creating cached field %s for %s", cachedFieldType.String(), state.BaseDir())
		state.cachedFields[cachedFieldType] = cachedFieldType.Make()
	}
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
	return state.GetCachedField(fieldType).(IterableCachedField)
}

// MergeIterableCachedFields will merge each IterableCachedField in the given ScoutState into the referred to ScoutState
// using the IterableCachedField.Merge method.
func (state *ScoutState) MergeIterableCachedFields(stateToMerge *ScoutState) {
	// First we merge each iterable cached field
	iterableCachedFields := stateToMerge.GetIterableCachedFields()
	for cachedFieldType, iterableCachedField := range iterableCachedFields {
		log.INFO.Printf("Merging %d %s into the ScoutState", iterableCachedField.Len(), cachedFieldType.String())
		state.GetIterableCachedField(cachedFieldType).Merge(iterableCachedField)
	}
}

// Load will deserialise the relevant files in BaseDir into the CachedField within the ScoutState. If the InMemory flag is
// set then this will do nothing.
func (state *ScoutState) Load() (err error) {
	if !state.InMemory {
		log.INFO.Printf("Loading ScoutState from %s", state.BaseDir())
		pathsToBytes := NewPathsToBytes(state.BaseDir())
		if err = pathsToBytes.LoadBaseDir(); err != nil {
			err = errors.Wrapf(err, "could not load ScoutState (%s) from disk", state.BaseDir())
		}

		for cachedFieldType, cacheField := range state.cachedFields {
			if err = cacheField.Deserialise(pathsToBytes); err != nil {
				err = errors.Wrapf(err, "could not deserialise cached field %s", cachedFieldType.String())
			}
		}
		pathsToBytes = nil
	}
	return
}

// Save will serialise all the CachedField to BaseDir, caching them persistently for later use. If the InMemory flag is
// set then this will do nothing.
func (state *ScoutState) Save() (err error) {
	if !state.InMemory {
		// We remove the previous save from the disk
		if err = os.RemoveAll(state.BaseDir()); err != nil {
			err = errors.Wrapf(err, "could not remove \"%s\" before saving", state.BaseDir())
			return
		}

		pathsToBytes := NewPathsToBytes(state.BaseDir())
		for cachedFieldType, cacheField := range state.cachedFields {
			switch cacheField.(type) {
			case IterableCachedField:
				log.INFO.Printf(
					"Serialising iterable field %s which contains %d",
					cachedFieldType.String(), cacheField.(IterableCachedField).Len(),
				)
			default:
				log.INFO.Printf("Serialising non-iterable field %s: %v", cachedFieldType.String(), cacheField)
			}
			if err = cacheField.Serialise(pathsToBytes); err != nil {
				err = errors.Wrapf(err, "could not serialise cached field %s", cachedFieldType.String())
				return
			}
		}

		// Finally, we write the PathsToBytes to disk
		if err = pathsToBytes.Write(); err != nil {
			err = errors.Wrap(err, "could not write serialised ScoutState to disk")
		}
		pathsToBytes = nil
	}
	return
}

// Delete will delete the cache by deleting the BaseDir (if it exists). If the InMemory flag is set then this will do
// nothing.
func (state *ScoutState) Delete() {
	if !state.InMemory {
		log.INFO.Printf("Deleting ScoutState cache %s", state.BaseDir())
		if _, err := os.Stat(state.BaseDir()); !os.IsNotExist(err) {
			_ = os.RemoveAll(state.BaseDir())
		} else {
			log.INFO.Printf("ScoutState cache %s does not exist", state.BaseDir())
		}
	}
}

// String returns the top-line information for the ScoutState.
func (state *ScoutState) String() string {
	start, _ := state.GetCachedField(StateType).Get("Start")
	finished, _ := state.GetCachedField(StateType).Get("Finished")
	phase, _ := state.GetCachedField(StateType).Get("Phase")
	phaseStart, _ := state.GetCachedField(StateType).Get("PhaseStart")
	batchSize, _ := state.GetCachedField(StateType).Get("BatchSize")
	discoveryTweets, _ := state.GetCachedField(StateType).Get("DiscoveryTweets")
	return fmt.Sprintf(`Directory: %s
Date: %s
Start: %s
Finished: %s
Phase: %v
Phase start: %s
Batch size: %d
Discovery tweets: %d
User tweet times: %d
Developer snapshots: %d
Game IDs: %d`,
		state.BaseDir(),
		state.createdTime.String(),
		start.(time.Time).String(),
		finished.(time.Time).String(),
		phase,
		phaseStart.(time.Time).String(),
		batchSize,
		discoveryTweets,
		state.GetIterableCachedField(UserTweetTimesType).Len(),
		state.GetIterableCachedField(DeveloperSnapshotsType).Len(),
		state.GetIterableCachedField(GameIDsType).Len(),
	)
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
	// Zero out the error so that we don't return it
	err = nil

	// We default to always creating a cache. If the newest cache time IsZero then the current date will be taken
	// anyway.
	state = newState(forceCreate, newestCacheTime)
	if !newestCacheTime.IsZero() {
		// If we did find an old cache then we will load it
		state.Loaded = true
		err = state.Load()
	}
	return
}

// StateInMemory will create a ScoutState that only exists in memory. Only the CachedField will be created and the
// ScoutState.InMemory flag set.
func StateInMemory() *ScoutState {
	state := &ScoutState{InMemory: true, cachedFields: make(map[CachedFieldType]CachedField)}
	state.makeCachedFields()
	return state
}
