export function fillPath(template, params) {
    return template.replace(/\{([^}]+)\}/g, (_matched, key) => {
        const value = params[key];
        if (value === undefined || value === null) {
            throw new Error(`missing path param: ${key}`);
        }
        return encodeURIComponent(String(value));
    });
}
export function appendQuery(path, query) {
    const entries = [];
    for (const [key, value] of Object.entries(query)) {
        if (Array.isArray(value)) {
            for (const item of value) {
                if (item === undefined || item === null) {
                    continue;
                }
                entries.push([key, String(item)]);
            }
            continue;
        }
        if (value === undefined || value === null) {
            continue;
        }
        entries.push([key, String(value)]);
    }
    if (entries.length === 0) {
        return path;
    }
    const search = new URLSearchParams(entries);
    return `${path}?${search.toString()}`;
}
