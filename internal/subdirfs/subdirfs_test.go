package subdirfs_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/sehrgutesoftware/httpdf/internal/subdirfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Run("it_creates_a_new_subdirfs_with_provided_root_path", func(t *testing.T) {
		tempDir := t.TempDir()

		subFS := subdirfs.New(tempDir)

		assert.NotNil(t, subFS)
		// Test that it implements the fs.SubFS interface
		var _ fs.SubFS = subFS
	})

	t.Run("it_creates_subdirfs_with_relative_path", func(t *testing.T) {
		subFS := subdirfs.New("relative/path")

		assert.NotNil(t, subFS)
		var _ fs.SubFS = subFS
	})

	t.Run("it_creates_subdirfs_with_absolute_path", func(t *testing.T) {
		tempDir := t.TempDir()
		absPath, err := filepath.Abs(tempDir)
		require.NoError(t, err)

		subFS := subdirfs.New(absPath)

		assert.NotNil(t, subFS)
		var _ fs.SubFS = subFS
	})
}

func TestSubDirFS_Sub(t *testing.T) {
	t.Run("it_creates_subfilesystem_for_existing_subdirectory", func(t *testing.T) {
		tempDir := t.TempDir()
		subDir := filepath.Join(tempDir, "subdir")
		err := os.Mkdir(subDir, 0755)
		require.NoError(t, err)

		subFS := subdirfs.New(tempDir)
		result, err := subFS.Sub("subdir")

		assert.NoError(t, err)
		assert.NotNil(t, result)

		// Verify it's also a SubFS
		resultSubFS, ok := result.(fs.SubFS)
		assert.True(t, ok)
		assert.NotNil(t, resultSubFS)
	})

	t.Run("it_creates_subfilesystem_for_nested_subdirectory", func(t *testing.T) {
		tempDir := t.TempDir()
		nestedDir := filepath.Join(tempDir, "level1", "level2")
		err := os.MkdirAll(nestedDir, 0755)
		require.NoError(t, err)

		subFS := subdirfs.New(tempDir)
		result, err := subFS.Sub("level1/level2")

		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("it_prevents_path_traversal_attacks_with_parent_directory_references", func(t *testing.T) {
		tempDir := t.TempDir()
		subDir := filepath.Join(tempDir, "allowed")
		err := os.Mkdir(subDir, 0755)
		require.NoError(t, err)

		subFS := subdirfs.New(tempDir)
		result, err := subFS.Sub("../../../etc")

		// Should still work but be contained within the root
		// The path traversal should be cleaned and contained
		assert.Error(t, err) // Should error because the traversed path doesn't exist
		assert.Nil(t, result)
	})

	t.Run("it_handles_current_directory_reference", func(t *testing.T) {
		tempDir := t.TempDir()

		subFS := subdirfs.New(tempDir)
		result, err := subFS.Sub(".")

		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("it_handles_path_with_multiple_slashes", func(t *testing.T) {
		tempDir := t.TempDir()
		subDir := filepath.Join(tempDir, "test")
		err := os.Mkdir(subDir, 0755)
		require.NoError(t, err)

		subFS := subdirfs.New(tempDir)
		result, err := subFS.Sub("//test//")

		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("it_returns_error_when_subdirectory_does_not_exist", func(t *testing.T) {
		tempDir := t.TempDir()

		subFS := subdirfs.New(tempDir)
		result, err := subFS.Sub("nonexistent")

		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("it_returns_error_when_path_points_to_file_not_directory", func(t *testing.T) {
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "testfile.txt")
		err := os.WriteFile(filePath, []byte("test content"), 0644)
		require.NoError(t, err)

		subFS := subdirfs.New(tempDir)
		result, err := subFS.Sub("testfile.txt")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "is not a directory")
	})

	t.Run("it_returns_error_when_root_directory_does_not_exist", func(t *testing.T) {
		subFS := subdirfs.New("/nonexistent/root/path")
		result, err := subFS.Sub("anydir")

		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("it_creates_subfilesystem_that_can_access_files", func(t *testing.T) {
		tempDir := t.TempDir()
		subDir := filepath.Join(tempDir, "testdir")
		err := os.Mkdir(subDir, 0755)
		require.NoError(t, err)

		testFile := filepath.Join(subDir, "test.txt")
		testContent := "Hello, World!"
		err = os.WriteFile(testFile, []byte(testContent), 0644)
		require.NoError(t, err)

		subFS := subdirfs.New(tempDir)
		result, err := subFS.Sub("testdir")
		require.NoError(t, err)

		// Try to read the file through the sub filesystem
		data, err := fs.ReadFile(result, "test.txt")
		assert.NoError(t, err)
		assert.Equal(t, testContent, string(data))
	})

	t.Run("it_creates_subfilesystem_that_can_list_directory_contents", func(t *testing.T) {
		tempDir := t.TempDir()
		subDir := filepath.Join(tempDir, "listtest")
		err := os.Mkdir(subDir, 0755)
		require.NoError(t, err)

		// Create test files
		testFiles := []string{"file1.txt", "file2.txt", "file3.txt"}
		for _, fileName := range testFiles {
			filePath := filepath.Join(subDir, fileName)
			err = os.WriteFile(filePath, []byte("content"), 0644)
			require.NoError(t, err)
		}

		subFS := subdirfs.New(tempDir)
		result, err := subFS.Sub("listtest")
		require.NoError(t, err)

		entries, err := fs.ReadDir(result, ".")
		assert.NoError(t, err)
		assert.Len(t, entries, 3)

		entryNames := make([]string, len(entries))
		for i, entry := range entries {
			entryNames[i] = entry.Name()
		}

		for _, expectedFile := range testFiles {
			assert.Contains(t, entryNames, expectedFile)
		}
	})

	t.Run("it_creates_nested_subfilesystems", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create nested directory structure
		level1 := filepath.Join(tempDir, "level1")
		level2 := filepath.Join(level1, "level2")
		level3 := filepath.Join(level2, "level3")
		err := os.MkdirAll(level3, 0755)
		require.NoError(t, err)

		// Create a test file in the deepest level
		testFile := filepath.Join(level3, "deep.txt")
		testContent := "Deep content"
		err = os.WriteFile(testFile, []byte(testContent), 0644)
		require.NoError(t, err)

		// Create first level subFS
		subFS := subdirfs.New(tempDir)
		level1FS, err := subFS.Sub("level1")
		require.NoError(t, err)

		// Create second level subFS
		level1SubFS, ok := level1FS.(fs.SubFS)
		require.True(t, ok)
		level2FS, err := level1SubFS.Sub("level2")
		require.NoError(t, err)

		// Create third level subFS
		level2SubFS, ok := level2FS.(fs.SubFS)
		require.True(t, ok)
		level3FS, err := level2SubFS.Sub("level3")
		require.NoError(t, err)

		// Read file from the deepest level
		data, err := fs.ReadFile(level3FS, "deep.txt")
		assert.NoError(t, err)
		assert.Equal(t, testContent, string(data))
	})

	t.Run("it_handles_empty_subdirectory_path", func(t *testing.T) {
		tempDir := t.TempDir()

		subFS := subdirfs.New(tempDir)
		result, err := subFS.Sub("")

		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("it_maintains_filesystem_interface_compatibility", func(t *testing.T) {
		tempDir := t.TempDir()
		subDir := filepath.Join(tempDir, "fstest")
		err := os.Mkdir(subDir, 0755)
		require.NoError(t, err)

		subFS := subdirfs.New(tempDir)
		result, err := subFS.Sub("fstest")
		require.NoError(t, err)

		// Test that all required filesystem operations work
		var _ fs.FS = result

		// Test that result implements fs.SubFS
		resultSubFS, ok := result.(fs.SubFS)
		assert.True(t, ok)
		var _ fs.SubFS = resultSubFS

		// Test ReadDir
		_, err = fs.ReadDir(result, ".")
		assert.NoError(t, err)

		// Test Stat (through fs.Stat)
		_, err = fs.Stat(result, ".")
		assert.NoError(t, err)
	})
}
