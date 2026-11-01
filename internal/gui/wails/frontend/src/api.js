// API client using Wails Go bindings instead of HTTP fetch.
export async function listDownloads() {
  const json = await window.go.backend.App.ListDownloads();
  return JSON.parse(json);
}

export async function addDownload(url, filename, dir, categoryId) {
  const json = await window.go.backend.App.AddDownload({
    url: url,
    filename: filename || undefined,
    dir: dir || undefined,
    categoryId: categoryId || undefined,
  });
  return JSON.parse(json);
}

export async function pauseDownload(id) {
  const json = await window.go.backend.App.PauseDownload(id);
  return JSON.parse(json);
}

export async function resumeDownload(id) {
  const json = await window.go.backend.App.ResumeDownload(id);
  return JSON.parse(json);
}

export async function cancelDownload(id) {
  const json = await window.go.backend.App.CancelDownload(id);
  return JSON.parse(json);
}

export async function removeDownload(id, deleteFile) {
  await window.go.backend.App.RemoveDownload(id, deleteFile);
}

export async function getSettings() {
  const json = await window.go.backend.App.GetSettings();
  return JSON.parse(json);
}

export async function updateSettings(settings) {
  const json = await window.go.backend.App.UpdateSettings(JSON.stringify(settings));
  return JSON.parse(json);
}

export async function listCategories() {
  const json = await window.go.backend.App.ListCategories();
  return JSON.parse(json);
}

export async function addCategory(name, defaultDir) {
  const json = await window.go.backend.App.AddCategory(name, defaultDir || undefined);
  return JSON.parse(json);
}

export async function deleteCategory(id) {
  await window.go.backend.App.DeleteCategory(id);
}
