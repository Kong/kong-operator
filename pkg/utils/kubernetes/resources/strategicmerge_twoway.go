// This is a copy from https://github.com/kubernetes/apimachinery/blob/v0.29.2/pkg/util/strategicpatch/patch.go#L139
// to make one small change which is not allowed by the API, namely: to set the diff option to ignore deletions.
package resources

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/util/mergepatch"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

const (
	directiveMarker  = "$patch"
	deleteDirective  = "delete"
	replaceDirective = "replace"
	mergeDirective   = "merge"

	retainKeysStrategy = "retainKeys"

	deleteFromPrimitiveListDirectivePrefix = "$deleteFromPrimitiveList"
	retainKeysDirective                    = "$" + retainKeysStrategy
	setElementOrderDirectivePrefix         = "$setElementOrder"
)

// CreateTwoWayMergeMapPatchUsingLookupPatchMeta is a copy of
// strategicpatch.CreateTwoWayMergeMapPatchUsingLookupPatchMeta with IgnoreDeletions
// set to true.
func CreateTwoWayMergeMapPatchUsingLookupPatchMeta(
	original, modified strategicpatch.JSONMap,
	schema strategicpatch.LookupPatchMeta,
	fns ...mergepatch.PreconditionFunc,
) (strategicpatch.JSONMap, error) {
	diffOptions := strategicpatch.DiffOptions{
		SetElementOrder: true,
		// NOTE: This is the only change made in this package.
		IgnoreDeletions: true,
	}
	patchMap, err := diffMaps(original, modified, schema, diffOptions)
	if err != nil {
		return nil, err
	}

	// Apply the preconditions to the patch, and return an error if any of them fail.
	for _, fn := range fns {
		if !fn(patchMap) {
			return nil, mergepatch.NewErrPreconditionFailed(patchMap)
		}
	}

	return patchMap, nil
}

// Returns a (recursive) strategic merge patch that yields modified when applied to original.
// Including:
// - Adding fields to the patch present in modified, missing from original
// - Setting fields to the patch present in modified and original with different values
// - Delete fields present in original, missing from modified through
// - IFF map field - set to nil in patch
// - IFF list of maps && merge strategy - use deleteDirective for the elements
// - IFF list of primitives && merge strategy - use parallel deletion list
// - IFF list of maps or primitives with replace strategy (default) - set patch value to the value in modified
// - Build $retainKeys directive for fields with retainKeys patch strategy
func diffMaps(
	original, modified map[string]interface{},
	schema strategicpatch.LookupPatchMeta,
	diffOptions strategicpatch.DiffOptions,
) (map[string]interface{}, error) {
	patch := map[string]interface{}{}

	// This will be used to build the $retainKeys directive sent in the patch
	retainKeysList := make([]interface{}, 0, len(modified))

	// Compare each value in the modified map against the value in the original map
	for key, modifiedValue := range modified {
		// Get the underlying type for pointers
		if diffOptions.BuildRetainKeysDirective && modifiedValue != nil {
			retainKeysList = append(retainKeysList, key)
		}

		originalValue, ok := original[key]
		if !ok {
			// Key was added, so add to patch
			if !diffOptions.IgnoreChangesAndAdditions {
				patch[key] = modifiedValue
			}
			continue
		}

		// The patch may have a patch directive
		// TODO: figure out if we need this. This shouldn't be needed by apply. When would the original map have patch directives in it?
		foundDirectiveMarker, err := handleDirectiveMarker(key, originalValue, modifiedValue, patch)
		if err != nil {
			return nil, err
		}
		if foundDirectiveMarker {
			continue
		}

		if reflect.TypeOf(originalValue) != reflect.TypeOf(modifiedValue) {
			// Types have changed, so add to patch
			if !diffOptions.IgnoreChangesAndAdditions {
				patch[key] = modifiedValue
			}
			continue
		}

		// Types are the same, so compare values
		switch originalValueTyped := originalValue.(type) {
		case map[string]interface{}:
			modifiedValueTyped := modifiedValue.(map[string]interface{})
			err = handleMapDiff(key, originalValueTyped, modifiedValueTyped, patch, schema, diffOptions)
		case []interface{}:
			modifiedValueTyped := modifiedValue.([]interface{})
			err = handleSliceDiff(key, originalValueTyped, modifiedValueTyped, patch, schema, diffOptions)
		default:
			replacePatchFieldIfNotEqual(key, originalValue, modifiedValue, patch, diffOptions)
		}
		if err != nil {
			return nil, err
		}
	}

	updatePatchIfMissing(original, modified, patch, diffOptions)
	// Insert the retainKeysList iff there are values present in the retainKeysList and
	// either of the following is true:
	// - the patch is not empty
	// - there are additional field in original that need to be cleared
	if len(retainKeysList) > 0 &&
		(len(patch) > 0 || hasAdditionalNewField(original, modified)) {
		patch[retainKeysDirective] = sortScalars(retainKeysList)
	}
	return patch, nil
}

// handleDirectiveMarker handles how to diff directive marker between 2 objects
func handleDirectiveMarker(key string, originalValue, modifiedValue interface{}, patch map[string]interface{}) (bool, error) {
	if key == directiveMarker {
		originalString, ok := originalValue.(string)
		if !ok {
			return false, fmt.Errorf("invalid value for special key: %s", directiveMarker)
		}
		modifiedString, ok := modifiedValue.(string)
		if !ok {
			return false, fmt.Errorf("invalid value for special key: %s", directiveMarker)
		}
		if modifiedString != originalString {
			patch[directiveMarker] = modifiedValue
		}
		return true, nil
	}
	return false, nil
}

// handleMapDiff diff between 2 maps `originalValueTyped` and `modifiedValue`,
// puts the diff in the `patch` associated with `key`
// key is the key associated with originalValue and modifiedValue.
// originalValue, modifiedValue are the old and new value respectively.They are both maps
// patch is the patch map that contains key and the updated value, and it is the parent of originalValue, modifiedValue
// diffOptions contains multiple options to control how we do the diff.
func handleMapDiff(key string, originalValue, modifiedValue, patch map[string]interface{},
	schema strategicpatch.LookupPatchMeta,
	diffOptions strategicpatch.DiffOptions,
) error {
	subschema, patchMeta, err := schema.LookupPatchMetadataForStruct(key)
	if err != nil {
		// We couldn't look up metadata for the field
		// If the values are identical, this doesn't matter, no patch is needed
		if reflect.DeepEqual(originalValue, modifiedValue) {
			return nil
		}
		// Otherwise, return the error
		return err
	}
	retainKeys, patchStrategy, err := extractRetainKeysPatchStrategy(patchMeta.GetPatchStrategies())
	if err != nil {
		return err
	}
	diffOptions.BuildRetainKeysDirective = retainKeys
	switch patchStrategy {
	// The patch strategic from metadata tells us to replace the entire object instead of diffing it
	case replaceDirective:
		if !diffOptions.IgnoreChangesAndAdditions {
			patch[key] = modifiedValue
		}
	default:
		patchValue, err := diffMaps(originalValue, modifiedValue, subschema, diffOptions)
		if err != nil {
			return err
		}
		// Maps were not identical, use provided patch value
		if len(patchValue) > 0 {
			patch[key] = patchValue
		}
	}
	return nil
}

// handleSliceDiff diff between 2 slices `originalValueTyped` and `modifiedValue`,
// puts the diff in the `patch` associated with `key`
// key is the key associated with originalValue and modifiedValue.
// originalValue, modifiedValue are the old and new value respectively.They are both slices
// patch is the patch map that contains key and the updated value, and it is the parent of originalValue, modifiedValue
// diffOptions contains multiple options to control how we do the diff.
func handleSliceDiff(key string, originalValue, modifiedValue []interface{}, patch map[string]interface{},
	schema strategicpatch.LookupPatchMeta,
	diffOptions strategicpatch.DiffOptions,
) error {
	subschema, patchMeta, err := schema.LookupPatchMetadataForSlice(key)
	if err != nil {
		// We couldn't look up metadata for the field
		// If the values are identical, this doesn't matter, no patch is needed
		if reflect.DeepEqual(originalValue, modifiedValue) {
			return nil
		}
		// Otherwise, return the error
		return err
	}
	retainKeys, patchStrategy, err := extractRetainKeysPatchStrategy(patchMeta.GetPatchStrategies())
	if err != nil {
		return err
	}
	switch patchStrategy {
	// Merge the 2 slices using mergePatchKey
	case mergeDirective:
		diffOptions.BuildRetainKeysDirective = retainKeys
		addList, deletionList, setOrderList, err := diffLists(originalValue, modifiedValue, subschema, patchMeta.GetPatchMergeKey(), diffOptions)
		if err != nil {
			return err
		}
		if len(addList) > 0 {
			patch[key] = addList
		}
		// generate a parallel list for deletion
		if len(deletionList) > 0 {
			parallelDeletionListKey := fmt.Sprintf("%s/%s", deleteFromPrimitiveListDirectivePrefix, key)
			patch[parallelDeletionListKey] = deletionList
		}
		if len(setOrderList) > 0 {
			parallelSetOrderListKey := fmt.Sprintf("%s/%s", setElementOrderDirectivePrefix, key)
			patch[parallelSetOrderListKey] = setOrderList
		}
	default:
		replacePatchFieldIfNotEqual(key, originalValue, modifiedValue, patch, diffOptions)
	}
	return nil
}

// extractRetainKeysPatchStrategy process patch strategy, which is a string may contains multiple
// patch strategies separated by ",". It returns a boolean var indicating if it has
// retainKeys strategies and a string for the other strategy.
func extractRetainKeysPatchStrategy(strategies []string) (bool, string, error) {
	switch len(strategies) {
	case 0:
		return false, "", nil
	case 1:
		singleStrategy := strategies[0]
		switch singleStrategy {
		case retainKeysStrategy:
			return true, "", nil
		default:
			return false, singleStrategy, nil
		}
	case 2:
		switch {
		case strategies[0] == retainKeysStrategy:
			return true, strategies[1], nil
		case strategies[1] == retainKeysStrategy:
			return true, strategies[0], nil
		default:
			return false, "", fmt.Errorf("unexpected patch strategy: %v", strategies)
		}
	default:
		return false, "", fmt.Errorf("unexpected patch strategy: %v", strategies)
	}
}

// replacePatchFieldIfNotEqual updates the patch if original and modified are not deep equal
// if diffOptions.IgnoreChangesAndAdditions is false.
// original is the old value, maybe either the live cluster object or the last applied configuration
// modified is the new value, is always the users new config
func replacePatchFieldIfNotEqual(key string, original, modified interface{}, patch map[string]interface{},
	diffOptions strategicpatch.DiffOptions,
) {
	if diffOptions.IgnoreChangesAndAdditions {
		// Ignoring changes - do nothing
		return
	}
	if reflect.DeepEqual(original, modified) {
		// Contents are identical - do nothing
		return
	}
	// Create a patch to replace the old value with the new one
	patch[key] = modified
}

// updatePatchIfMissing iterates over `original` when ignoreDeletions is false.
// Clear the field whose key is not present in `modified`.
// original is the old value, maybe either the live cluster object or the last applied configuration
// modified is the new value, is always the users new config
func updatePatchIfMissing(original, modified, patch map[string]interface{},
	diffOptions strategicpatch.DiffOptions,
) {
	if diffOptions.IgnoreDeletions {
		// Ignoring deletion - do nothing
		return
	}
	// Add nils for deleted values
	for key := range original {
		if _, found := modified[key]; !found {
			patch[key] = nil
		}
	}
}

// Returns a (recursive) strategic merge patch, a parallel deletion list if necessary and
// another list to set the order of the list
// Only list of primitives with merge strategy will generate a parallel deletion list.
// These two lists should yield modified when applied to original, for lists with merge semantics.
func diffLists(original, modified []interface{}, schema strategicpatch.LookupPatchMeta, mergeKey string, diffOptions strategicpatch.DiffOptions) ([]interface{}, []interface{}, []interface{}, error) {
	if len(original) == 0 {
		// Both slices are empty - do nothing
		if len(modified) == 0 || diffOptions.IgnoreChangesAndAdditions {
			return nil, nil, nil, nil
		}

		// Old slice was empty - add all elements from the new slice
		return modified, nil, nil, nil
	}

	elementType, err := sliceElementType(original, modified)
	if err != nil {
		return nil, nil, nil, err
	}

	var patchList, deleteList, setOrderList []interface{}
	kind := elementType.Kind()
	switch kind {
	case reflect.Map:
		patchList, deleteList, err = diffListsOfMaps(original, modified, schema, mergeKey, diffOptions)
		if err != nil {
			return nil, nil, nil, err
		}
		patchList, err = normalizeSliceOrder(patchList, modified, mergeKey, kind)
		if err != nil {
			return nil, nil, nil, err
		}
		orderSame, err := isOrderSame(original, modified, mergeKey)
		if err != nil {
			return nil, nil, nil, err
		}
		// append the deletions to the end of the patch list.
		patchList = append(patchList, deleteList...)
		deleteList = nil
		// generate the setElementOrder list when there are content changes or order changes
		if diffOptions.SetElementOrder &&
			((!diffOptions.IgnoreChangesAndAdditions && (len(patchList) > 0 || !orderSame)) ||
				(!diffOptions.IgnoreDeletions && len(patchList) > 0)) {
			// Generate a list of maps that each item contains only the merge key.
			setOrderList = make([]interface{}, len(modified))
			for i, v := range modified {
				typedV := v.(map[string]interface{})
				setOrderList[i] = map[string]interface{}{
					mergeKey: typedV[mergeKey],
				}
			}
		}
	case reflect.Slice:
		// Lists of Lists are not permitted by the api
		return nil, nil, nil, mergepatch.ErrNoListOfLists
	default:
		patchList, deleteList, err = diffListsOfScalars(original, modified, diffOptions)
		if err != nil {
			return nil, nil, nil, err
		}
		patchList, err = normalizeSliceOrder(patchList, modified, mergeKey, kind)
		// generate the setElementOrder list when there are content changes or order changes
		if diffOptions.SetElementOrder && ((!diffOptions.IgnoreDeletions && len(deleteList) > 0) ||
			(!diffOptions.IgnoreChangesAndAdditions && !reflect.DeepEqual(original, modified))) {
			setOrderList = modified
		}
	}
	return patchList, deleteList, setOrderList, err
}

func sortScalars(s []interface{}) []interface{} {
	ss := SortableSliceOfScalars{s}
	sort.Sort(ss)
	return ss.s
}

// hasAdditionalNewField returns if original map has additional key with non-nil value than modified.
func hasAdditionalNewField(original, modified map[string]interface{}) bool {
	for k, v := range original {
		if v == nil {
			continue
		}
		if _, found := modified[k]; !found {
			return true
		}
	}
	return false
}

// Returns the type of the elements of N slice(s). If the type is different,
// another slice or undefined, returns an error.
func sliceElementType(slices ...[]interface{}) (reflect.Type, error) {
	var prevType reflect.Type
	for _, s := range slices {
		// Go through elements of all given slices and make sure they are all the same type.
		for _, v := range s {
			currentType := reflect.TypeOf(v)
			if prevType == nil {
				prevType = currentType
				// We don't support lists of lists yet.
				if prevType.Kind() == reflect.Slice {
					return nil, mergepatch.ErrNoListOfLists
				}
			} else {
				if prevType != currentType {
					return nil, fmt.Errorf("list element types are not identical: %v", fmt.Sprint(slices))
				}
				prevType = currentType
			}
		}
	}

	if prevType == nil {
		return nil, fmt.Errorf("no elements in any of the given slices")
	}

	return prevType, nil
}

// diffListsOfMaps takes a pair of lists and
// returns a (recursive) strategic merge patch list contains additions and changes and
// a deletion list contains deletions
func diffListsOfMaps(original, modified []interface{}, schema strategicpatch.LookupPatchMeta, mergeKey string, diffOptions strategicpatch.DiffOptions) ([]interface{}, []interface{}, error) {
	patch := make([]interface{}, 0, len(modified))
	deletionList := make([]interface{}, 0, len(original))

	originalSorted, err := sortMergeListsByNameArray(original, schema, mergeKey, false)
	if err != nil {
		return nil, nil, err
	}
	modifiedSorted, err := sortMergeListsByNameArray(modified, schema, mergeKey, false)
	if err != nil {
		return nil, nil, err
	}

	originalIndex, modifiedIndex := 0, 0
	for {
		originalInBounds := originalIndex < len(originalSorted)
		modifiedInBounds := modifiedIndex < len(modifiedSorted)
		bothInBounds := originalInBounds && modifiedInBounds
		if !originalInBounds && !modifiedInBounds {
			break
		}

		var originalElementMergeKeyValueString, modifiedElementMergeKeyValueString string
		var originalElementMergeKeyValue, modifiedElementMergeKeyValue interface{}
		var originalElement, modifiedElement map[string]interface{}
		if originalInBounds {
			originalElement, originalElementMergeKeyValue, err = getMapAndMergeKeyValueByIndex(originalIndex, mergeKey, originalSorted)
			if err != nil {
				return nil, nil, err
			}
			originalElementMergeKeyValueString = fmt.Sprintf("%v", originalElementMergeKeyValue)
		}
		if modifiedInBounds {
			modifiedElement, modifiedElementMergeKeyValue, err = getMapAndMergeKeyValueByIndex(modifiedIndex, mergeKey, modifiedSorted)
			if err != nil {
				return nil, nil, err
			}
			modifiedElementMergeKeyValueString = fmt.Sprintf("%v", modifiedElementMergeKeyValue)
		}

		switch {
		case bothInBounds && strategicpatch.ItemMatchesOriginalAndModifiedSlice(originalElementMergeKeyValueString, modifiedElementMergeKeyValueString):
			// Merge key values are equal, so recurse
			patchValue, err := diffMaps(originalElement, modifiedElement, schema, diffOptions)
			if err != nil {
				return nil, nil, err
			}
			if len(patchValue) > 0 {
				patchValue[mergeKey] = modifiedElementMergeKeyValue
				patch = append(patch, patchValue)
			}
			originalIndex++
			modifiedIndex++
		// only modified is in bound
		case !originalInBounds:
			fallthrough
		// modified has additional map
		case bothInBounds && strategicpatch.ItemAddedToModifiedSlice(originalElementMergeKeyValueString, modifiedElementMergeKeyValueString):
			if !diffOptions.IgnoreChangesAndAdditions {
				patch = append(patch, modifiedElement)
			}
			modifiedIndex++
		// only original is in bound
		case !modifiedInBounds:
			fallthrough
		// original has additional map
		case bothInBounds && strategicpatch.ItemRemovedFromModifiedSlice(originalElementMergeKeyValueString, modifiedElementMergeKeyValueString):
			if !diffOptions.IgnoreDeletions {
				// Item was deleted, so add delete directive
				deletionList = append(deletionList, strategicpatch.CreateDeleteDirective(mergeKey, originalElementMergeKeyValue))
			}
			originalIndex++
		}
	}

	return patch, deletionList, nil
}

// normalizeSliceOrder sort `toSort` list by `order`
func normalizeSliceOrder(toSort, order []interface{}, mergeKey string, kind reflect.Kind) ([]interface{}, error) {
	var toDelete []interface{}
	if kind == reflect.Map {
		// make sure each item in toSort, order has merge key
		err := validateMergeKeyInLists(mergeKey, toSort, order)
		if err != nil {
			return nil, err
		}
		toSort, toDelete, err = extractToDeleteItems(toSort)
		if err != nil {
			return nil, err
		}
	}

	sort.SliceStable(toSort, func(i, j int) bool {
		if ii := index(order, toSort[i], mergeKey, kind); ii >= 0 {
			if ij := index(order, toSort[j], mergeKey, kind); ij >= 0 {
				return ii < ij
			}
		}
		return true
	})
	toSort = append(toSort, toDelete...)
	return toSort, nil
}

// validateMergeKeyInLists checks if each map in the list has the mentryerge key.
func validateMergeKeyInLists(mergeKey string, lists ...[]interface{}) error {
	for _, list := range lists {
		for _, item := range list {
			m, ok := item.(map[string]interface{})
			if !ok {
				return mergepatch.ErrBadArgType(m, item)
			}
			if _, ok = m[mergeKey]; !ok {
				return mergepatch.ErrNoMergeKey(m, mergeKey)
			}
		}
	}
	return nil
}

// index returns the index of the item in the given items, or -1 if it doesn't exist
// l must NOT be a slice of slices, this should be checked before calling.
func index(l []interface{}, valToLookUp interface{}, mergeKey string, kind reflect.Kind) int {
	var getValFn func(interface{}) interface{}
	// Get the correct `getValFn` based on item `kind`.
	// It should return the value of merge key for maps and
	// return the item for other kinds.
	switch kind {
	case reflect.Map:
		getValFn = func(item interface{}) interface{} {
			typedItem, ok := item.(map[string]interface{})
			if !ok {
				return nil
			}
			val := typedItem[mergeKey]
			return val
		}
	default:
		getValFn = func(item interface{}) interface{} {
			return item
		}
	}

	for i, v := range l {
		if getValFn(valToLookUp) == getValFn(v) {
			return i
		}
	}
	return -1
}

// isOrderSame checks if the order in a list has changed
func isOrderSame(original, modified []interface{}, mergeKey string) (bool, error) {
	if len(original) != len(modified) {
		return false, nil
	}
	for i, modifiedItem := range modified {
		equal, err := mergeKeyValueEqual(original[i], modifiedItem, mergeKey)
		if err != nil || !equal {
			return equal, err
		}
	}
	return true, nil
}

func mergeKeyValueEqual(left, right interface{}, mergeKey string) (bool, error) {
	if len(mergeKey) == 0 {
		return left == right, nil
	}
	typedLeft, ok := left.(map[string]interface{})
	if !ok {
		return false, mergepatch.ErrBadArgType(typedLeft, left)
	}
	typedRight, ok := right.(map[string]interface{})
	if !ok {
		return false, mergepatch.ErrBadArgType(typedRight, right)
	}
	mergeKeyLeft, ok := typedLeft[mergeKey]
	if !ok {
		return false, mergepatch.ErrNoMergeKey(typedLeft, mergeKey)
	}
	mergeKeyRight, ok := typedRight[mergeKey]
	if !ok {
		return false, mergepatch.ErrNoMergeKey(typedRight, mergeKey)
	}
	return mergeKeyLeft == mergeKeyRight, nil
}

// diffListsOfScalars returns 2 lists, the first one is addList and the second one is deletionList.
// Argument diffOptions.IgnoreChangesAndAdditions controls if calculate addList. true means not calculate.
// Argument diffOptions.IgnoreDeletions controls if calculate deletionList. true means not calculate.
// original may be changed, but modified is guaranteed to not be changed
func diffListsOfScalars(original, modified []interface{}, diffOptions strategicpatch.DiffOptions) ([]interface{}, []interface{}, error) {
	modifiedCopy := make([]interface{}, len(modified))
	copy(modifiedCopy, modified)
	// Sort the scalars for easier calculating the diff
	originalScalars := sortScalars(original)
	modifiedScalars := sortScalars(modifiedCopy)

	originalIndex, modifiedIndex := 0, 0
	addList := []interface{}{}
	deletionList := []interface{}{}

	for {
		originalInBounds := originalIndex < len(originalScalars)
		modifiedInBounds := modifiedIndex < len(modifiedScalars)
		if !originalInBounds && !modifiedInBounds {
			break
		}
		// we need to compare the string representation of the scalar,
		// because the scalar is an interface which doesn't support either < or >
		// And that's how func sortScalars compare scalars.
		var originalString, modifiedString string
		var originalValue, modifiedValue interface{}
		if originalInBounds {
			originalValue = originalScalars[originalIndex]
			originalString = fmt.Sprintf("%v", originalValue)
		}
		if modifiedInBounds {
			modifiedValue = modifiedScalars[modifiedIndex]
			modifiedString = fmt.Sprintf("%v", modifiedValue)
		}

		originalV, modifiedV := compareListValuesAtIndex(originalInBounds, modifiedInBounds, originalString, modifiedString)
		switch {
		case originalV == nil && modifiedV == nil:
			originalIndex++
			modifiedIndex++
		case originalV != nil && modifiedV == nil:
			if !diffOptions.IgnoreDeletions {
				deletionList = append(deletionList, originalValue)
			}
			originalIndex++
		case originalV == nil && modifiedV != nil:
			if !diffOptions.IgnoreChangesAndAdditions {
				addList = append(addList, modifiedValue)
			}
			modifiedIndex++
		default:
			return nil, nil, fmt.Errorf("Unexpected returned value from compareListValuesAtIndex: %v and %v", originalV, modifiedV)
		}
	}

	return addList, deduplicateScalars(deletionList), nil
}

// If first return value is non-nil, list1 contains an element not present in list2
// If second return value is non-nil, list2 contains an element not present in list1
func compareListValuesAtIndex(list1Inbounds, list2Inbounds bool, list1Value, list2Value string) (interface{}, interface{}) {
	bothInBounds := list1Inbounds && list2Inbounds
	switch {
	// scalars are identical
	case bothInBounds && list1Value == list2Value:
		return nil, nil
	// only list2 is in bound
	case !list1Inbounds:
		fallthrough
	// list2 has additional scalar
	case bothInBounds && list1Value > list2Value:
		return nil, list2Value
	// only original is in bound
	case !list2Inbounds:
		fallthrough
	// original has additional scalar
	case bothInBounds && list1Value < list2Value:
		return list1Value, nil
	default:
		return nil, nil
	}
}

func deduplicateScalars(s []interface{}) []interface{} {
	// Clever algorithm to deduplicate.
	length := len(s) - 1
	for i := 0; i < length; i++ {
		for j := i + 1; j <= length; j++ {
			if s[i] == s[j] {
				s[j] = s[length]
				s = s[0:length]
				length--
				j--
			}
		}
	}

	return s
}

// extractToDeleteItems takes a list and
// returns 2 lists: one contains items that should be kept and the other contains items to be deleted.
func extractToDeleteItems(l []interface{}) ([]interface{}, []interface{}, error) {
	var nonDelete, toDelete []interface{}
	for _, v := range l {
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil, nil, mergepatch.ErrBadArgType(m, v)
		}

		directive, foundDirective := m[directiveMarker]
		if foundDirective && directive == deleteDirective {
			toDelete = append(toDelete, v)
		} else {
			nonDelete = append(nonDelete, v)
		}
	}
	return nonDelete, toDelete, nil
}

// getMapAndMergeKeyValueByIndex return a map in the list and its merge key value given the index of the map.
func getMapAndMergeKeyValueByIndex(index int, mergeKey string, listOfMaps []interface{}) (map[string]interface{}, interface{}, error) {
	m, ok := listOfMaps[index].(map[string]interface{})
	if !ok {
		return nil, nil, mergepatch.ErrBadArgType(m, listOfMaps[index])
	}

	val, ok := m[mergeKey]
	if !ok {
		return nil, nil, mergepatch.ErrNoMergeKey(m, mergeKey)
	}
	return m, val, nil
}

// Function sortMergeListsByNameMap recursively sorts the merge lists by its mergeKey in an array.
func sortMergeListsByNameArray(s []interface{}, schema strategicpatch.LookupPatchMeta, mergeKey string, recurse bool) ([]interface{}, error) {
	if len(s) == 0 {
		return s, nil
	}

	// We don't support lists of lists yet.
	t, err := sliceElementType(s)
	if err != nil {
		return nil, err
	}

	// If the elements are not maps...
	if t.Kind() != reflect.Map {
		// Sort the elements, because they may have been merged out of order.
		return deduplicateAndSortScalars(s), nil
	}

	// Elements are maps - if one of the keys of the map is a map or a
	// list, we may need to recurse into it.
	newS := []interface{}{}
	for _, elem := range s {
		if recurse {
			typedElem := elem.(map[string]interface{})
			newElem, err := sortMergeListsByNameMap(typedElem, schema)
			if err != nil {
				return nil, err
			}

			newS = append(newS, newElem)
		} else {
			newS = append(newS, elem)
		}
	}

	// Sort the maps.
	newS = sortMapsBasedOnField(newS, mergeKey)
	return newS, nil
}

func deduplicateAndSortScalars(s []interface{}) []interface{} {
	s = deduplicateScalars(s)
	return sortScalars(s)
}

func sortMapsBasedOnField(m []interface{}, fieldName string) []interface{} {
	mapM := mapSliceFromSlice(m)
	ss := SortableSliceOfMaps{mapM, fieldName}
	sort.Sort(ss)
	newS := sliceFromMapSlice(ss.s)
	return newS
}

func sliceFromMapSlice(s []map[string]interface{}) []interface{} {
	newS := []interface{}{}
	for _, v := range s {
		newS = append(newS, v)
	}

	return newS
}

func mapSliceFromSlice(m []interface{}) []map[string]interface{} {
	newM := []map[string]interface{}{}
	for _, v := range m {
		vt := v.(map[string]interface{})
		newM = append(newM, vt)
	}

	return newM
}

type SortableSliceOfMaps struct {
	s []map[string]interface{}
	k string // key to sort on
}

func (ss SortableSliceOfMaps) Len() int {
	return len(ss.s)
}

func (ss SortableSliceOfMaps) Less(i, j int) bool {
	iStr := fmt.Sprintf("%v", ss.s[i][ss.k])
	jStr := fmt.Sprintf("%v", ss.s[j][ss.k])
	return sort.StringsAreSorted([]string{iStr, jStr})
}

func (ss SortableSliceOfMaps) Swap(i, j int) {
	tmp := ss.s[i]
	ss.s[i] = ss.s[j]
	ss.s[j] = tmp
}

// Function sortMergeListsByNameMap recursively sorts the merge lists by its mergeKey in a map.
func sortMergeListsByNameMap(s map[string]interface{}, schema strategicpatch.LookupPatchMeta) (map[string]interface{}, error) {
	newS := map[string]interface{}{}
	for k, v := range s {
		if k == retainKeysDirective {
			typedV, ok := v.([]interface{})
			if !ok {
				return nil, mergepatch.ErrBadPatchFormatForRetainKeys
			}
			v = sortScalars(typedV)
		} else if strings.HasPrefix(k, deleteFromPrimitiveListDirectivePrefix) {
			typedV, ok := v.([]interface{})
			if !ok {
				return nil, mergepatch.ErrBadPatchFormatForPrimitiveList
			}
			v = sortScalars(typedV)
		} else if strings.HasPrefix(k, setElementOrderDirectivePrefix) {
			_, ok := v.([]interface{})
			if !ok {
				return nil, mergepatch.ErrBadPatchFormatForSetElementOrderList
			}
		} else if k != directiveMarker {
			// recurse for map and slice.
			switch typedV := v.(type) {
			case map[string]interface{}:
				subschema, _, err := schema.LookupPatchMetadataForStruct(k)
				if err != nil {
					return nil, err
				}
				v, err = sortMergeListsByNameMap(typedV, subschema)
				if err != nil {
					return nil, err
				}
			case []interface{}:
				subschema, patchMeta, err := schema.LookupPatchMetadataForSlice(k)
				if err != nil {
					return nil, err
				}
				_, patchStrategy, err := extractRetainKeysPatchStrategy(patchMeta.GetPatchStrategies())
				if err != nil {
					return nil, err
				}
				if patchStrategy == mergeDirective {
					var err error
					v, err = sortMergeListsByNameArray(typedV, subschema, patchMeta.GetPatchMergeKey(), true)
					if err != nil {
						return nil, err
					}
				}
			}
		}

		newS[k] = v
	}

	return newS, nil
}

type SortableSliceOfScalars struct {
	s []interface{}
}

func (ss SortableSliceOfScalars) Len() int {
	return len(ss.s)
}

func (ss SortableSliceOfScalars) Less(i, j int) bool {
	iStr := fmt.Sprintf("%v", ss.s[i])
	jStr := fmt.Sprintf("%v", ss.s[j])
	return sort.StringsAreSorted([]string{iStr, jStr})
}

func (ss SortableSliceOfScalars) Swap(i, j int) {
	tmp := ss.s[i]
	ss.s[i] = ss.s[j]
	ss.s[j] = tmp
}
