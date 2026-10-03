const BLOCKED_REDIRECT_PATHS = ["/login", "/login-verification", "/setup"];

const ASSET_RECOVERY_KEY = 'smail:route-asset-recovery';
const READ_ONLY_ROUTES = new Set(['home', 'list', 'detail']);
const RECOVERABLE_ROUTES = new Set([...READ_ONLY_ROUTES, 'editer']);

function isRouteAssetLoadError(error) {
    const message = typeof error?.message === 'string' ? error.message : '';
    return /^Unable to preload CSS for /.test(message)
        || /^Failed to fetch dynamically imported module:/.test(message)
        || /^error loading dynamically imported module/i.test(message)
        || /^Importing a module script failed/.test(message)
        || (error?.name === 'ChunkLoadError' && /^Loading chunk /.test(message));
}

function isReadOnlyRoute(route) {
    return READ_ONLY_ROUTES.has(route?.name);
}

function createRouteAssetRecovery({buildID, getStorage, reload}) {
    let recovering = false;

    return function recoverRouteAsset(error, to, from, {editing = false, online = true} = {}) {
        if (!isRouteAssetLoadError(error) || !isReadOnlyRoute(from) || editing || !online) {
            return false;
        }
        let recoveryPath = to?.fullPath;
        if (to?.name === 'login' && to.path === '/login') {
            // 旧邮件页登录失效时也可能缺失登录资源，只重建安全的登录目标。
            const query = new URLSearchParams({redirect: getSafeLoginRedirect(to.query?.redirect)});
            recoveryPath = `/login?${query}`;
        } else if (!RECOVERABLE_ROUTES.has(to?.name) || !recoveryPath
            || getSafeLoginRedirect(recoveryPath, '') !== recoveryPath) {
            return false;
        }
        if (recovering) {
            return true;
        }
        try {
            const storage = getStorage();
            if (!buildID || storage.getItem(ASSET_RECOVERY_KEY) === buildID) {
                return false;
            }
            // 先持久化重试标记；同一标签页的同一构建最多自动恢复一次。
            storage.setItem(ASSET_RECOVERY_KEY, buildID);
            recovering = true;
            reload(recoveryPath);
            return true;
        } catch {
            // 浏览器禁用存储或导航失败时保留手动恢复入口，不能无限刷新。
            recovering = false;
            return false;
        }
    };
}

function decodeRedirect(value) {
    let decoded = value;
    for (let attempt = 0; attempt < 2; attempt += 1) {
        try {
            const next = decodeURIComponent(decoded);
            if (next === decoded) {
                break;
            }
            decoded = next;
        } catch {
            break;
        }
    }
    return decoded;
}

function getSafeLoginRedirect(value, fallback = "/") {
    if (typeof value !== "string") {
        return fallback;
    }

    const redirect = value.trim();
    if (!redirect.startsWith("/") || redirect.startsWith("//")) {
        return fallback;
    }

    const decodedRedirect = decodeRedirect(redirect);
    if (!decodedRedirect.startsWith("/") || decodedRedirect.startsWith("//") || decodedRedirect.includes("\\")) {
        return fallback;
    }

    const decodedPath = decodedRedirect
        .split(/[?#]/, 1)[0]
        .replace(/\/+$/, "")
        .toLowerCase() || "/";

    if (BLOCKED_REDIRECT_PATHS.some((path) => decodedPath === path || decodedPath.startsWith(`${path}/`))) {
        return fallback;
    }

    return redirect;
}

function createLoginLocation(redirect) {
    return {
        name: "login",
        query: {
            redirect: getSafeLoginRedirect(redirect)
        }
    };
}

function createSetupLocation(redirect) {
    return {
        name: "setup",
        query: {
            redirect: getSafeLoginRedirect(redirect)
        }
    };
}

function getCurrentLoginRedirect(route) {
    if (route?.name === "login" || route?.name === "login-verification") {
        return getSafeLoginRedirect(route?.query?.redirect);
    }
    return getSafeLoginRedirect(route?.fullPath);
}

function createLoginRedirectHandler(router) {
    let redirectPromise = null;

    return function redirectToLogin() {
        const currentRoute = router.currentRoute.value;
        if (currentRoute?.name === "login" || currentRoute?.name === "login-verification") {
            return Promise.resolve(false);
        }
        if (redirectPromise) {
            return redirectPromise;
        }

        const location = createLoginLocation(getCurrentLoginRedirect(currentRoute));
        redirectPromise = Promise.resolve(router.replace(location))
            .then(() => true)
            .finally(() => {
                redirectPromise = null;
            });

        return redirectPromise;
    };
}

export {
    createRouteAssetRecovery,
    isReadOnlyRoute,
    isRouteAssetLoadError,
    createLoginLocation,
    createLoginRedirectHandler,
    createSetupLocation,
    getCurrentLoginRedirect,
    getSafeLoginRedirect
};
