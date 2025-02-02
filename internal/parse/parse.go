package parse

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/upsidr/importer/internal/errorsplus"
	"github.com/upsidr/importer/internal/file"
	"github.com/upsidr/importer/internal/marker"
	"github.com/upsidr/importer/internal/regexpplus"
)

var (
	ErrUnsupportedFileType = errors.New("unsupported file type")
	ErrNoInput             = errors.New("no file content found")
	ErrInvalidPath         = errors.New("invalid path provided")
	ErrInvalidSyntax       = errors.New("invalid syntax given")
	ErrDuplicatedMarker    = errors.New("duplicated marker within a single file")
)

// Parse reads filename and input, and parses data in the file.
//
// The steps are as follows:
//
// 	1. Read input data
// 	2. Scan each line
// 	3. Look for regex match for marker
// 	4. Save matched line number and options found
// 	5. Verify parsed data, and return
//
// If any of the above steps failed, it would return an error. This function
// does not populate the ContentAfter.
//
// TODO: Consider merging with the private parse method, as this public
//       function is not adding any value at the moment.
func Parse(fileName string, input io.Reader) (*file.File, error) {
	if input == nil {
		return nil, ErrNoInput
	}

	return parse(fileName, input)
}

// parse reads file input using scanner. This reads the input line by line, and
// store the data into File data. Parsing the data stores 3 sets of data: file
// content as is, marker details, and file content with all data between
// marker pairs purged.
func parse(fileName string, input io.Reader) (*file.File, error) {
	var importerMarkerRegex string
	var importerSkipMarker string

	fileType := filepath.Ext(fileName)
	switch fileType {
	case ".md":
		importerMarkerRegex = marker.ImporterMarkerMarkdown
		importerSkipMarker = marker.ImporterSkipProcessingMarkdown
	case ".yaml", ".yml":
		importerMarkerRegex = marker.ImporterMarkerYAML
		importerSkipMarker = marker.ImporterSkipProcessingYAML
	default:
		return nil, fmt.Errorf("%w, '%s' provided", ErrUnsupportedFileType, fileType)
	}

	f := &file.File{
		FileName: fileName,

		// NOTE:
		// For *File.contentXyz, I'm purposely making the first item in slice empty
		// for readability. This shouldn't be necessary, but with this approach,
		// the slice index matches the line number, and is easy to get my head
		// around for now.
		ContentBefore: make([]string, 0),
		ContentPurged: make([]string, 0),
	}

	markers := map[int]*marker.Marker{}
	rawMarkers := map[string]*marker.RawMarker{}

	currentLine := 0
	inNested := false // Flag to check if the data is between markers
	nestedUnder := "" // Name to check for marker pair ending

	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		currentLine++
		currentStr := scanner.Text()
		f.ContentBefore = append(f.ContentBefore, currentStr)

		// If skip marker is found, turn on the flag. This flag should disable
		// in-place file update, but should not suppress generate or preview.
		if strings.Contains(currentStr, importerSkipMarker) {
			f.SkipUpdate = true
		}

		// Look for marker match
		matches, err := regexpplus.MapWithNamedSubgroups(currentStr, importerMarkerRegex)
		if err != nil {
			if errors.Is(err, regexpplus.ErrNoMatch) {
				// If the line appears within some other marker set, remove the line.
				if inNested {
					continue
				}
				// Otherwise ensure the marker itself is a part of purged data.
				f.ContentPurged = append(f.ContentPurged, currentStr)

				// There is no further action needed for matched line, and thus continue.
				continue
			}

			panic(err) // Unknown error, should not happen
		}

		var subgroupName string
		if importerName, found := matches["importer_name"]; found {
			subgroupName = importerName
		}

		// Ensure this is the top most marker. If a nested marker is found
		// within another marker, ignore it. This is because nested markers
		// should be handled in those target files instead.
		//
		// This means, in any file in question, the parse logic only looks at
		// one file and its direct dependencies, and does not try to reconcile
		// nested dependencies.
		//
		// TODO: Handle nested file dependencies with AST
		if inNested && nestedUnder != subgroupName {
			continue
		}

		nestedUnder = subgroupName

		// At this point, the marker is important, and we need to process
		// the line further.
		// Note that, ContentPurged does not contain any data that's wrapped
		// between markers. Those lines will be kept as an empty byte slice
		// for further processing later to create ContentAfter.
		f.ContentPurged = append(f.ContentPurged, currentStr)

		// Markers must match up to create a pair. If it isn't a proper
		// pair, it is treated as broken. For that reason, we need to keep
		// track of already found match.
		matchData := &marker.RawMarker{Name: subgroupName}
		if data, found := rawMarkers[subgroupName]; found {
			// If a marker with the same name has a pair already, return as an error.
			// TODO: multiple 'begin' marker may still be a problem
			if data.IsBeginFound && data.IsEndFound {
				return nil, fmt.Errorf("%w, marker '%s' has been already processed", ErrDuplicatedMarker, subgroupName)
			}
			matchData = data
		}

		if importerMarker, found := matches["importer_marker"]; found {
			switch importerMarker {
			case "begin":
				inNested = true
				matchData.IsBeginFound = true
				matchData.LineToInsertAt = len(f.ContentPurged)
			case "end":
				inNested = false
				nestedUnder = ""
				matchData.IsEndFound = true
				continue
			default:
				panic("unknown marker condition") // Should not happen, but putting this for possible future changes
			}
		}
		if importerOption, found := matches["importer_option"]; found && importerOption != "" {
			// skipping empty string as end marker shouldn't override
			matchData.Options = importerOption
		}
		if markerIndentation, found := matches["importer_marker_indentation"]; found && markerIndentation != "" {
			// skipping empty string as end marker shouldn't override
			matchData.PrecedingIndentation = markerIndentation
		}

		rawMarkers[subgroupName] = matchData
	}

	errs := errorsplus.Errors{}
	for _, data := range rawMarkers {
		marker, err := marker.NewMarker(data)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		markers[marker.LineToInsertAt] = marker
	}
	if len(errs) != 0 {
		return nil, errs
	}

	f.Markers = markers

	return f, nil
}
