const AUTH_STATUS = Object.freeze({
    UNKNOWN: "unknown",
    LOADING: "loading",
    AUTHENTICATED: "authenticated",
    UNAUTHENTICATED: "unauthenticated",
    NEEDS_SETUP: "needs_setup",
    UNAVAILABLE: "unavailable"
});

let authRefreshPromise = null;

function classifyAuthenticationResponse(response) {
    if (response?.errorNo === 402) {
        return AUTH_STATUS.NEEDS_SETUP;
    }
    if (response?.errorNo === 0 && response.data && typeof response.data === "object") {
        return AUTH_STATUS.AUTHENTICATED;
    }
    if (response?.errorNo === 403) {
        return AUTH_STATUS.UNAUTHENTICATED;
    }
    return AUTH_STATUS.UNAVAILABLE;
}

function classifyAuthenticationError(error, options = {}) {
    if (error?.response?.data?.errorNo === 402 || error?.response?.status === 402) {
        return AUTH_STATUS.NEEDS_SETUP;
    }
    if (error?.response?.data?.errorNo === 403
        || (options.authenticationRequest && error?.response?.status === 403)) {
        return AUTH_STATUS.UNAUTHENTICATED;
    }
    return AUTH_STATUS.UNAVAILABLE;
}

function runSharedAuthRefresh(task) {
    if (authRefreshPromise) {
        return authRefreshPromise;
    }

    authRefreshPromise = Promise.resolve()
        .then(task)
        .finally(() => {
            authRefreshPromise = null;
        });

    return authRefreshPromise;
}

export {
    AUTH_STATUS,
    classifyAuthenticationError,
    classifyAuthenticationResponse,
    runSharedAuthRefresh
};
