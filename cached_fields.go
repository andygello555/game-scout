package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/gotils/v2/slices"
	"github.com/deckarep/golang-set/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// CachedField represents a field in ScoutState that can be cached on-disk using the PathsToBytesDirectory lookup.
type CachedField interface {
	// Type returns the CachedFieldType for this CachedField.
	Type() CachedFieldType
	// BaseDir is the parent directory that the cached field will be stored in when serialised and written to disk.
	BaseDir() string
	// Path takes string arguments and returns the path that the cached field will be written to after being
	// serialised. This path should have the BaseDir as a prefix. The given arguments can be used within the path
	// returned by the procedure in any way that is necessary.
	Path(paths ...string) string
	// Serialise will serialise the CachedField instance to bytes and add the resulting files to the PathsToBytesWriter.
	// In this case PathsToBytesDirectory only needs to be write-only. If the instance is going to be serialised to a directory
	// structure then remember to add placeholder/metadata files in the root of each directory so that the directories
	// are created by PathsToBytesWriter and the structure can be deserialised.
	Serialise(pathsToBytes PathsToBytesWriter) error
	// Deserialise will deserialise the bytes pertaining to this CachedField by looking up the necessary files from the
	// given PathsToBytesReader instance. In this case, PathsToBytesDirectory only needs to be read-only.
	Deserialise(pathsToBytes PathsToBytesReader) error
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
	Iter() CachedFieldIterator
	// Merge will perform a union/update procedure between the referred to IterableCachedField and the given
	// CachedField.
	Merge(field CachedField)
}

type CachedFieldIterator interface {
	// Continue checks whether the CachedFieldIterator has not finished. I.e. there are still more elements to iterate over.
	Continue() bool
	// I will return the current i (index) value.
	I() int
	// Key will return the current key of the element that is being iterated over.
	Key() any
	// Get will return the current element that is being iterated over.
	Get() (any, bool)
	// Next will dequeue the current element. This should be called at the end of each loop.
	Next()
	// Field will return the IterableCachedField that this CachedFieldIterator is iterating over.
	Field() IterableCachedField
	// Len returns the length of the CachedFieldIterator by calling IterableCachedField.Len.
	Len() int
}

// CachedFieldIterator is the iterator pattern that is returned by the IterableCachedField.Iter method.
type cachedFieldIterator struct {
	i           int
	cachedField IterableCachedField
	queue       []any
}

func (cfi *cachedFieldIterator) Continue() bool             { return len(cfi.queue) != 0 }
func (cfi *cachedFieldIterator) I() int                     { return cfi.i }
func (cfi *cachedFieldIterator) Key() any                   { return cfi.queue[0] }
func (cfi *cachedFieldIterator) Get() (any, bool)           { return cfi.cachedField.Get(cfi.Key()) }
func (cfi *cachedFieldIterator) Next()                      { cfi.i++; cfi.queue = cfi.queue[1:] }
func (cfi *cachedFieldIterator) Field() IterableCachedField { return cfi.cachedField }
func (cfi *cachedFieldIterator) Len() int                   { return cfi.cachedField.Len() }

type mergedCachedFieldIterator struct {
	its       []CachedFieldIterator
	idx       int
	completed int
}

func (m *mergedCachedFieldIterator) Continue() bool {
	// If the current iterator can continue, or the next iterator can continue (if there is one)
	if len(m.its) == 0 {
		return false
	} else if m.its[m.idx].Continue() {
		return true
	} else if m.idx < len(m.its)-1 && m.its[m.idx+1].Continue() {
		// We increment the current iterator ptr if there is another iterator that has not yet been started
		m.completed += m.its[m.idx].Len()
		m.idx++
		return true
	}
	return false
}

func (m *mergedCachedFieldIterator) I() int                     { return m.completed + m.its[m.idx].I() }
func (m *mergedCachedFieldIterator) Key() any                   { return m.its[m.idx].Key() }
func (m *mergedCachedFieldIterator) Get() (any, bool)           { return m.its[m.idx].Get() }
func (m *mergedCachedFieldIterator) Next()                      { m.its[m.idx].Next() }
func (m *mergedCachedFieldIterator) Field() IterableCachedField { return m.its[m.idx].Field() }

func (m *mergedCachedFieldIterator) Len() int {
	length := 0
	for _, field := range m.its {
		length += field.Len()
	}
	return length
}

func MergeCachedFieldIterators(iterators ...CachedFieldIterator) CachedFieldIterator {
	return &mergedCachedFieldIterator{
		its: iterators,
		idx: 0,
	}
}

var developerTypeUserTimesPrefix = map[models.DeveloperType]string{
	models.UnknownDeveloperType: "unknownUserPost",
	models.TwitterDeveloperType: "userTweet",
	models.RedditDeveloperType:  "redditUserPost",
}

func userTimesName(devType models.DeveloperType) string {
	return fmt.Sprintf("%sTimes", developerTypeUserTimesPrefix[devType])
}

func userTimesTitle(devType models.DeveloperType) string {
	return cases.Title(language.Und).String(userTimesName(devType))
}

func userTimesPath(devType models.DeveloperType, paths ...string) string {
	return fmt.Sprintf("%s.json", userTimesName(devType))
}

func userTimesSerialise(ut IterableCachedField, devType models.DeveloperType, writer PathsToBytesWriter) (err error) {
	var data []byte
	if data, err = json.Marshal(&ut); err != nil {
		err = errors.Wrapf(err, "cannot serialise %s", userTimesTitle(devType))
		return
	}
	writer.AddFilenameBytes(ut.Path(), data)
	return
}

func userTimesDeserialise(ut IterableCachedField, devType models.DeveloperType, reader PathsToBytesReader) (err error) {
	var data []byte
	if data, err = reader.BytesForFilename(ut.Path()); err != nil {
		err = errors.Wrapf(err, "%s does not exist in cache", userTimesTitle(devType))
		return
	}
	if err = json.Unmarshal(data, ut); err != nil {
		err = errors.Wrap(err, "could not deserialise UserTweetTimes")
		return
	}
	return
}

func userTimesSetOrAdd(ut map[string][]time.Time, args ...any) {
	for argNo := 0; argNo < len(args); argNo += 2 {
		developerID := args[argNo].(string)
		postTime := args[argNo+1].(time.Time)
		if _, ok := ut[developerID]; !ok {
			ut[developerID] = make([]time.Time, 0)
		}
		ut[developerID] = append(ut[developerID], postTime)
	}
}

func userTimesIter(ut any) CachedFieldIterator {
	utCached := ut.(IterableCachedField)
	iter := &cachedFieldIterator{
		cachedField: utCached,
		queue:       make([]any, utCached.Len()),
	}

	var utMap map[string][]time.Time
	switch ut.(type) {
	case *UserTweetTimes:
		utMap = *ut.(*UserTweetTimes)
	case *RedditUserPostTimes:
		utMap = *ut.(*RedditUserPostTimes)
	default:
		panic(errors.New("user times are not *UserTweetTimes or *RedditUserPostTimes"))
	}

	i := 0
	for key := range utMap {
		iter.queue[i] = key
		i++
	}
	return iter
}

func userTimesMerge(ut map[string][]time.Time, field CachedField) {
	var fieldMap map[string][]time.Time
	switch field.(type) {
	case *UserTweetTimes:
		fieldMap = *field.(*UserTweetTimes)
	case *RedditUserPostTimes:
		fieldMap = *field.(*RedditUserPostTimes)
	}

	for developerID, tweetTimes := range fieldMap {
		if _, ok := ut[developerID]; !ok {
			ut[developerID] = make([]time.Time, len(tweetTimes))
			copy(ut[developerID], tweetTimes)
		} else {
			ut[developerID] = append(ut[developerID], tweetTimes...)
		}
	}
}

// UserTweetTimes is used in the Scout procedure to store the times of the tweets for a Twitter user. The keys are
// comprised of Twitter user IDs. UserTweetTimes are serialised straight to JSON.
type UserTweetTimes map[string][]time.Time

func (utt *UserTweetTimes) Type() CachedFieldType { return UserTweetTimesType }

func (utt *UserTweetTimes) BaseDir() string { return "" }

func (utt *UserTweetTimes) Path(paths ...string) string {
	return userTimesPath(models.TwitterDeveloperType, paths...)
}

func (utt *UserTweetTimes) Serialise(pathsToBytes PathsToBytesWriter) error {
	return userTimesSerialise(utt, models.TwitterDeveloperType, pathsToBytes)
}

func (utt *UserTweetTimes) Deserialise(pathsToBytes PathsToBytesReader) error {
	return userTimesDeserialise(utt, models.TwitterDeveloperType, pathsToBytes)
}

func (utt *UserTweetTimes) SetOrAdd(args ...any) { userTimesSetOrAdd(*utt, args...) }

func (utt *UserTweetTimes) Len() int { return len(*utt) }

func (utt *UserTweetTimes) Iter() CachedFieldIterator { return userTimesIter(utt) }

func (utt *UserTweetTimes) Get(key any) (any, bool) { return (*utt)[key.(string)], false }

func (utt *UserTweetTimes) Merge(field CachedField) { userTimesMerge(*utt, field) }

// RedditUserPostTimes is used in the Scout procedure to store the times of the posts for a Reddit user. The keys are
// comprised of Reddit user IDs. RedditUserPostTimes are serialised straight to JSON.
type RedditUserPostTimes map[string][]time.Time

func (r *RedditUserPostTimes) Type() CachedFieldType { return RedditUserPostTimesType }

func (r *RedditUserPostTimes) BaseDir() string { return "" }

func (r *RedditUserPostTimes) Path(paths ...string) string {
	return userTimesPath(models.RedditDeveloperType, paths...)
}

func (r *RedditUserPostTimes) Serialise(pathsToBytes PathsToBytesWriter) error {
	return userTimesSerialise(r, models.RedditDeveloperType, pathsToBytes)
}

func (r *RedditUserPostTimes) Deserialise(pathsToBytes PathsToBytesReader) error {
	return userTimesDeserialise(r, models.RedditDeveloperType, pathsToBytes)
}

func (r *RedditUserPostTimes) SetOrAdd(args ...any) { userTimesSetOrAdd(*r, args...) }

func (r *RedditUserPostTimes) Get(key any) (any, bool) { return (*r)[key.(string)], false }

func (r *RedditUserPostTimes) Len() int { return len(*r) }

func (r *RedditUserPostTimes) Iter() CachedFieldIterator { return userTimesIter(r) }

func (r *RedditUserPostTimes) Merge(field CachedField) { userTimesMerge(*r, field) }

var developerTypeSnapshotsPrefix = map[models.DeveloperType]string{
	models.UnknownDeveloperType: "unknownDeveloperSnapshots",
	models.TwitterDeveloperType: "developerSnapshots",
	models.RedditDeveloperType:  "redditDeveloperSnapshots",
}

func devSnapsBaseDir(devType models.DeveloperType) string {
	return developerTypeSnapshotsPrefix[devType]
}

func devSnapsPath(ds IterableCachedField, paths ...string) string {
	return filepath.Join(ds.BaseDir(), strings.Join(paths, "_")+binExtension)
}

func devSnapsSerialise(ds any, devType models.DeveloperType, writer PathsToBytesWriter) (err error) {
	dsCached := ds.(IterableCachedField)
	// We create an info file so that BaseDir is always created
	writer.AddFilenameBytes(
		filepath.Join(dsCached.BaseDir(), "meta.json"),
		[]byte(fmt.Sprintf(
			"{\"type\": %q, \"count\": %d, \"ids\": [%s]}",
			devType.String(),
			dsCached.Len(),
			strings.Join(
				strings.Split(
					strings.Trim(
						fmt.Sprintf(
							"%v",
							dsCached.Iter().(*cachedFieldIterator).queue,
						),
						"[]",
					),
					" ",
				),
				", ",
			),
		)),
	)

	var dsMap map[string][]*models.DeveloperSnapshot
	switch ds.(type) {
	case *DeveloperSnapshots:
		dsMap = *ds.(*DeveloperSnapshots)
	case *RedditDeveloperSnapshots:
		dsMap = *ds.(*RedditDeveloperSnapshots)
	}

	for developerID, snapshots := range dsMap {
		var data bytes.Buffer
		enc := gob.NewEncoder(&data)
		if err = enc.Encode(snapshots); err != nil {
			err = errors.Wrapf(err, "could not encode %d snapshots for %s developer \"%s\"", len(snapshots), devType.String(), developerID)
			return
		}
		writer.AddFilenameBytes(dsCached.Path(developerID), data.Bytes())
	}
	return
}

func devSnapsDeserialise(ds any, devType models.DeveloperType, reader PathsToBytesReader) (err error) {
	dsCached := ds.(IterableCachedField)
	var dsMap map[string][]*models.DeveloperSnapshot
	switch ds.(type) {
	case *DeveloperSnapshots:
		dsMap = *ds.(*DeveloperSnapshots)
	case *RedditDeveloperSnapshots:
		dsMap = *ds.(*RedditDeveloperSnapshots)
	}
	dsFtb := reader.BytesForDirectory(dsCached.BaseDir())
	for filename, data := range dsFtb.inner {
		if filepath.Base(filename) != "meta.json" {
			// Trim the PathToBytes BaseDir, the DeveloperSnapshots BaseDir, and the file extension
			developerID := strings.TrimSuffix(filepath.Base(filename), binExtension)
			dec := gob.NewDecoder(bytes.NewReader(data))
			var snapshots []*models.DeveloperSnapshot
			if err = dec.Decode(&snapshots); err != nil {
				err = errors.Wrapf(err, "cannot deserialise snapshots for %s developer \"%s\"", devType.String(), developerID)
				return
			}

			if _, ok := dsMap[developerID]; !ok {
				dsMap[developerID] = make([]*models.DeveloperSnapshot, len(snapshots))
				copy(dsMap[developerID], snapshots)
			} else {
				dsMap[developerID] = append(dsMap[developerID], snapshots...)
			}
		}
	}
	dsFtb = nil
	return
}

func devSnapsSetOrAdd(ds map[string][]*models.DeveloperSnapshot, args ...any) {
	for argNo := 0; argNo < len(args); argNo += 2 {
		developerID := args[argNo].(string)
		snapshot := args[argNo+1].(*models.DeveloperSnapshot)
		if _, ok := ds[developerID]; !ok {
			ds[developerID] = make([]*models.DeveloperSnapshot, 0)
		}
		ds[developerID] = append(ds[developerID], snapshot)
	}
}

func devSnapsIter(ds any) CachedFieldIterator {
	dsCached := ds.(IterableCachedField)
	iter := &cachedFieldIterator{
		cachedField: dsCached,
		queue:       make([]any, dsCached.Len()),
	}

	var dsMap map[string][]*models.DeveloperSnapshot
	switch ds.(type) {
	case *DeveloperSnapshots:
		dsMap = *ds.(*DeveloperSnapshots)
	case *RedditDeveloperSnapshots:
		dsMap = *ds.(*RedditDeveloperSnapshots)
	}

	i := 0
	for key := range dsMap {
		iter.queue[i] = key
		i++
	}
	return iter
}

func devSnapsMerge(ds map[string][]*models.DeveloperSnapshot, field CachedField) {
	var fieldMap map[string][]*models.DeveloperSnapshot
	switch field.(type) {
	case *DeveloperSnapshots:
		fieldMap = *field.(*DeveloperSnapshots)
	case *RedditDeveloperSnapshots:
		fieldMap = *field.(*RedditDeveloperSnapshots)
	}

	for developerID, snapshots := range fieldMap {
		if _, ok := ds[developerID]; !ok {
			ds[developerID] = make([]*models.DeveloperSnapshot, len(snapshots))
			copy(ds[developerID], snapshots)
		} else {
			ds[developerID] = append(ds[developerID], snapshots...)
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

func (ds *DeveloperSnapshots) Type() CachedFieldType { return DeveloperSnapshotsType }

func (ds *DeveloperSnapshots) BaseDir() string { return devSnapsBaseDir(models.TwitterDeveloperType) }

func (ds *DeveloperSnapshots) Path(paths ...string) string { return devSnapsPath(ds, paths...) }

func (ds *DeveloperSnapshots) Serialise(pathsToBytes PathsToBytesWriter) error {
	return devSnapsSerialise(ds, models.TwitterDeveloperType, pathsToBytes)
}

func (ds *DeveloperSnapshots) Deserialise(pathsToBytes PathsToBytesReader) error {
	return devSnapsDeserialise(ds, models.TwitterDeveloperType, pathsToBytes)
}

func (ds *DeveloperSnapshots) SetOrAdd(args ...any) { devSnapsSetOrAdd(*ds, args...) }

func (ds *DeveloperSnapshots) Len() int { return len(*ds) }

func (ds *DeveloperSnapshots) Iter() CachedFieldIterator { return devSnapsIter(ds) }

func (ds *DeveloperSnapshots) Get(key any) (any, bool) { return (*ds)[key.(string)], false }

func (ds *DeveloperSnapshots) Merge(field CachedField) { devSnapsMerge(*ds, field) }

type RedditDeveloperSnapshots map[string][]*models.DeveloperSnapshot

func (r *RedditDeveloperSnapshots) Type() CachedFieldType { return RedditDeveloperSnapshotsType }

func (r *RedditDeveloperSnapshots) BaseDir() string {
	return devSnapsBaseDir(models.RedditDeveloperType)
}

func (r *RedditDeveloperSnapshots) Path(paths ...string) string { return devSnapsPath(r, paths...) }

func (r *RedditDeveloperSnapshots) Serialise(pathsToBytes PathsToBytesWriter) error {
	return devSnapsSerialise(r, models.RedditDeveloperType, pathsToBytes)
}

func (r *RedditDeveloperSnapshots) Deserialise(pathsToBytes PathsToBytesReader) error {
	return devSnapsDeserialise(r, models.RedditDeveloperType, pathsToBytes)
}

func (r *RedditDeveloperSnapshots) SetOrAdd(args ...any) { devSnapsSetOrAdd(*r, args...) }

func (r *RedditDeveloperSnapshots) Get(key any) (any, bool) { return (*r)[key.(string)], false }

func (r *RedditDeveloperSnapshots) Len() int { return len(*r) }

func (r *RedditDeveloperSnapshots) Iter() CachedFieldIterator { return devSnapsIter(r) }

func (r *RedditDeveloperSnapshots) Merge(field CachedField) { devSnapsMerge(*r, field) }

// GameIDs is the set of all models.Game IDs that have been scraped/updated so far in the Scout procedure. GameIDs is
// serialised to a JSON array.
type GameIDs struct {
	mapset.Set[uuid.UUID]
}

func (ids *GameIDs) Type() CachedFieldType { return GameIDsType }

func (ids *GameIDs) BaseDir() string { return "" }

func (ids *GameIDs) Path(paths ...string) string { return "gameIDs.json" }

func (ids *GameIDs) Serialise(pathsToBytes PathsToBytesWriter) (err error) {
	var data []byte
	if data, err = json.Marshal(&ids); err != nil {
		err = errors.Wrap(err, "cannot serialise GameIDs")
		return
	}
	pathsToBytes.AddFilenameBytes(ids.Path(), data)
	return
}

func (ids *GameIDs) Deserialise(pathsToBytes PathsToBytesReader) (err error) {
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

func (ids *GameIDs) Iter() CachedFieldIterator {
	cfi := &cachedFieldIterator{
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

func (d *DeletedDevelopers) Type() CachedFieldType { return DeletedDevelopersType }

func (d *DeletedDevelopers) BaseDir() string { return "deletedDevelopers" }

func (d *DeletedDevelopers) Path(paths ...string) string {
	return filepath.Join(d.BaseDir(), strings.Join(paths, "_")+binExtension)
}

func (d *DeletedDevelopers) Serialise(pathsToBytes PathsToBytesWriter) (err error) {
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

func (d *DeletedDevelopers) Deserialise(pathsToBytes PathsToBytesReader) (err error) {
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

func (d *DeletedDevelopers) Iter() CachedFieldIterator {
	cfi := &cachedFieldIterator{
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
	UpdatedDevelopers []models.DeveloperMinimal
	// DisabledDevelopers are the IDs of the models.Developer that have been disabled in a previous DisablePhase. This
	// applies to both the Disable and Enable Phase.
	DisabledDevelopers []models.DeveloperMinimal
	// EnabledDevelopers are the IDs of the models.Developer that were re-enabled in a previous EnablePhase. This
	// applies to only the Enable Phase.
	EnabledDevelopers []models.DeveloperMinimal
	// DevelopersToEnable is the number of developers to go to be re-enabled in the EnablePhase. This applies only to
	// the Enable Phase.
	DevelopersToEnable map[models.DeveloperType]int
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

func (s *State) Type() CachedFieldType { return StateType }

func (s *State) BaseDir() string { return "" }

func (s *State) Path(paths ...string) string { return "state.json" }

func (s *State) Serialise(pathsToBytes PathsToBytesWriter) (err error) {
	var data []byte
	if data, err = json.Marshal(s); err != nil {
		err = errors.Wrap(err, "cannot serialise State")
		return
	}
	pathsToBytes.AddFilenameBytes(s.Path(), data)
	return
}

func (s *State) Deserialise(pathsToBytes PathsToBytesReader) (err error) {
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
		s.UpdatedDevelopers = append(s.UpdatedDevelopers, models.DeveloperMinimal{
			ID:   args[1].(string),
			Type: args[2].(models.DeveloperType),
		})
	case "DisabledDevelopers":
		// We will append an array of IDs onto the DeletedDevelopers
		s.DisabledDevelopers = append(s.DisabledDevelopers, args[1].([]models.DeveloperMinimal)...)
	case "EnabledDevelopers":
		s.EnabledDevelopers = append(s.EnabledDevelopers, models.DeveloperMinimal{
			ID:   args[1].(string),
			Type: args[2].(models.DeveloperType),
		})
	case "DevelopersToEnable":
		s.DevelopersToEnable[args[1].(models.DeveloperType)] = args[2].(int)
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
	case "TUpdatedDevelopers", "RUpdatedDevelopers":
		devType, _ := models.DevTypeFromUsername(key.(string))
		return slices.Filter(s.UpdatedDevelopers, func(idx int, value models.DeveloperMinimal, arr []models.DeveloperMinimal) bool {
			return value.Type == devType
		}), true
	case "DisabledDevelopers":
		return s.DisabledDevelopers, true
	case "TDisabledDevelopers", "RDisabledDevelopers":
		devType, _ := models.DevTypeFromUsername(key.(string))
		return slices.Filter(s.DisabledDevelopers, func(idx int, value models.DeveloperMinimal, arr []models.DeveloperMinimal) bool {
			return value.Type == devType
		}), true
	case "EnabledDevelopers":
		return s.EnabledDevelopers, true
	case "TEnabledDevelopers", "REnabledDevelopers":
		devType, _ := models.DevTypeFromUsername(key.(string))
		return slices.Filter(s.EnabledDevelopers, func(idx int, value models.DeveloperMinimal, arr []models.DeveloperMinimal) bool {
			return value.Type == devType
		}), true
	case "DevelopersToEnable":
		return s.DevelopersToEnable, true
	case "TDevelopersToEnable", "RDevelopersToEnable":
		devType, _ := models.DevTypeFromUsername(key.(string))
		count, ok := s.DevelopersToEnable[devType]
		return count, ok
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
	// UserTweetTimesType is the type of UserTweetTimes, a mapping of Developer IDs to arrays of times on which a post
	// was created by that Developer. The Developer IDs are prefixed with the Developer's models.DeveloperType character.
	UserTweetTimesType CachedFieldType = iota
	// RedditUserPostTimesType is the type of RedditUserPostTimes, which is identical to UserTweetTimes but for Reddit
	// Developers rather than Twitter ones.
	RedditUserPostTimesType
	// DeveloperSnapshotsType is the type of DeveloperSnapshots, a mapping of Developer IDs to arrays of partial
	// DeveloperSnapshots that will be aggregated in the Snapshot Phase. The Developer IDs are prefixed with the
	// Developer's models.DeveloperType character.
	DeveloperSnapshotsType
	// RedditDeveloperSnapshotsType is the type of RedditDeveloperSnapshots, which is identical to DeveloperSnapshots
	// but for Reddit Developers rather than Twitter ones.
	RedditDeveloperSnapshotsType
	// GameIDsType is the type of GameIDs, a set of UUIDs for each scraped models.Game.
	GameIDsType
	// DeletedDevelopersType is the type of DeletedDevelopers, a list of models.TrendingDev that will be deleted at the
	// end of the Delete Phase and collected within the Measure Phase.
	DeletedDevelopersType
	// StateType is the type of State, a struct containing a variety of fields that keep track of a Scout procedure
	// execution.
	StateType
)

// Make will return a new instance of the CachedField for the given CachedFieldType.
func (cft CachedFieldType) Make() CachedField {
	switch cft {
	case UserTweetTimesType:
		utt := make(UserTweetTimes)
		return &utt
	case RedditUserPostTimesType:
		rutt := make(RedditUserPostTimes)
		return &rutt
	case DeveloperSnapshotsType:
		ds := make(DeveloperSnapshots)
		return &ds
	case RedditDeveloperSnapshotsType:
		rds := make(RedditDeveloperSnapshots)
		return &rds
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
			UpdatedDevelopers:  make([]models.DeveloperMinimal, 0),
			DisabledDevelopers: make([]models.DeveloperMinimal, 0),
			EnabledDevelopers:  make([]models.DeveloperMinimal, 0),
			DevelopersToEnable: map[models.DeveloperType]int{
				models.TwitterDeveloperType: 0,
				models.RedditDeveloperType:  0,
				models.UnknownDeveloperType: 0,
			},
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
	case RedditUserPostTimesType:
		return "RedditUserPostTimes"
	case DeveloperSnapshotsType:
		return "DeveloperSnapshots"
	case RedditDeveloperSnapshotsType:
		return "RedditDeveloperSnapshots"
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
		RedditUserPostTimesType,
		DeveloperSnapshotsType,
		RedditDeveloperSnapshotsType,
		GameIDsType,
		DeletedDevelopersType,
		StateType,
	}
}
