const AUTH_STATUS = Object.freeze({
    UNKNOWN: "unknown",
    LOADING: "loading",
    AUTHENTICATED: "authenticated",
    UNAUTHENTICATED: "unauthenticated",
    UNAVAILABLE: "unavailable"
});

let authRefreshPromise = null;

function classifyAuthenticationResponse(response) {
    if (response?.errorNo === 0 && response.data && typeof response.data === "object") {
        return AUTH_STATUS.AUTHENTICATED;
    }
    if (response?.errorNo === 403) {
        return AUTH_STATUS.UNAUTHENTICATED;
    }
    return AUTH_STATUS.UNAVAILABLE;
}

function classifyAuthenticationError(error) {
    if (error?.response?.status === 403 || error?.response?.data?.errorNo === 403) {
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
