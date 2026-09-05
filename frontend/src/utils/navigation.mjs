const BLOCKED_REDIRECT_PATHS = ["/login", "/login-verification", "/setup"];

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
    createLoginLocation,
    createLoginRedirectHandler,
    createSetupLocation,
    getCurrentLoginRedirect,
    getSafeLoginRedirect
};
