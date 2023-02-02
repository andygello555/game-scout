package main

import (
	"bytes"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/email"
	"github.com/pkg/errors"
	"html/template"
	"os"
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

// deserialiseFields will deserialise all the cachedFields and store them back in the given ScoutState.
func (state *ScoutState) deserialiseFields(p PathsToBytesReader) (err error) {
	for cachedFieldType, cacheField := range state.cachedFields {
		if err = cacheField.Deserialise(p); err != nil {
			err = errors.Wrapf(err, "could not deserialise cached field %s", cachedFieldType.String())
			return
		}
	}
	return
}

// Load will deserialise the relevant files in BaseDir into the CachedField within the ScoutState. If the StateInMemory flag is
// set then this will do nothing.
func (state *ScoutState) Load() (err error) {
	if !state.InMemory {
		log.INFO.Printf("Loading ScoutState from %s", state.BaseDir())
		pathsToBytes := NewPathsToBytes(state.BaseDir())
		if err = state.LoadFrom(pathsToBytes); err != nil {
			return
		}
		pathsToBytes = nil
	}
	return
}

// LoadFrom loads the ScoutState from the given PathsToBytesReader.
func (state *ScoutState) LoadFrom(p PathsToBytesReader) (err error) {
	if err = p.LoadBaseDir(); err != nil {
		err = errors.Wrapf(err, "could not load ScoutState (%s) from disk", p.BaseDir())
		return
	}
	err = state.deserialiseFields(p)
	return
}

// serialiseFields will serialise and store all cachedFields within the given PathsToBytesWriter.
func (state *ScoutState) serialiseFields(p PathsToBytesWriter) (err error) {
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
		if err = cacheField.Serialise(p); err != nil {
			err = errors.Wrapf(err, "could not serialise cached field %s", cachedFieldType.String())
			return
		}
	}
	return
}

// Save will serialise all the CachedField to BaseDir, caching them persistently for later use. If the StateInMemory flag is
// set then this will do nothing.
func (state *ScoutState) Save() (err error) {
	if !state.InMemory {
		// We remove the previous save from the disk
		if err = os.RemoveAll(state.BaseDir()); err != nil {
			err = errors.Wrapf(err, "could not remove \"%s\" before saving", state.BaseDir())
			return
		}

		// Save the State to a newly created PathsToBytesDirectory reader
		pathsToBytes := NewPathsToBytes(state.BaseDir())
		if err = state.SaveTo(pathsToBytes); err != nil {
			return
		}
		pathsToBytes = nil
	}
	return
}

// SaveTo will save the ScoutState to the given PathsToBytesWriter.
func (state *ScoutState) SaveTo(p PathsToBytesWriter) (err error) {
	if err = state.serialiseFields(p); err != nil {
		return
	}
	if err = p.Write(); err != nil {
		err = errors.Wrap(err, "could not write serialised ScoutState to disk")
	}
	return
}

// SaveToPart will save the ScoutState to a ZIP archive then wrap the archive buffer in an email.Part so that the
// ScoutState can be sent in an email.Email. Note: the returned email.Part will have the DropIfBig flag set.
func (state *ScoutState) SaveToPart() (part email.Part, err error) {
	archive := newPathsToBytesZipped()
	if err = state.SaveTo(archive); err != nil {
		err = errors.Wrap(err, "could not save ScoutState to ZIP archive")
		return
	}
	part = email.Part{
		Buffer:     bytes.NewReader(archive.buf.Bytes()),
		Attachment: true,
		Filename:   "scout_state.zip",
		DropIfBig:  true,
	}
	return
}

// Delete will delete the cache by deleting the BaseDir (if it exists). If the StateInMemory flag is set then this will do
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

var stateContextFuncMap = map[string]any{
	"stamp": func(t time.Time) string {
		return t.Format(time.Stamp)
	},
	"mustGet": func(field CachedField, key string) any {
		v, _ := field.Get(key)
		return v
	},
}

type StartedContext struct {
	State *ScoutState
}

func (s *StartedContext) Path() email.TemplatePath { return email.Started }
func (s *StartedContext) Execute() *email.Template { return s.Template().Execute() }
func (s *StartedContext) Funcs() template.FuncMap  { return stateContextFuncMap }
func (s *StartedContext) Template() *email.Template {
	return email.NewParsedTemplate(email.Text, s).Template(s)
}
func (s *StartedContext) AdditionalParts() (parts []email.Part, err error) {
	parts = make([]email.Part, 1)
	parts[0], err = s.State.SaveToPart()
	return
}

type ErrorContext struct {
	Time  time.Time
	Error error
	State *ScoutState
}

func (e *ErrorContext) Path() email.TemplatePath { return email.Error }
func (e *ErrorContext) Execute() *email.Template { return e.Template().Execute() }
func (e *ErrorContext) Funcs() template.FuncMap  { return stateContextFuncMap }
func (e *ErrorContext) Template() *email.Template {
	return email.NewParsedTemplate(email.Text, e).Template(e)
}
func (e *ErrorContext) AdditionalParts() (parts []email.Part, err error) {
	parts = make([]email.Part, 1)
	parts[0], err = e.State.SaveToPart()
	return
}
