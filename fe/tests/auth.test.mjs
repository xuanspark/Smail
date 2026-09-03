import assert from "node:assert/strict";
import test from "node:test";

import {
    AUTH_STATUS,
    classifyAuthenticationError,
    classifyAuthenticationResponse,
    runSharedAuthRefresh
} from "../src/utils/auth.mjs";

test("认证响应会区分已登录、未登录和服务不可用", () => {
    assert.equal(
        classifyAuthenticationResponse({errorNo: 0, data: {account: "user@example.com"}}),
        AUTH_STATUS.AUTHENTICATED
    );
    assert.equal(
        classifyAuthenticationResponse({errorNo: 403}),
        AUTH_STATUS.UNAUTHENTICATED
    );
    assert.equal(
        classifyAuthenticationResponse({errorNo: 500}),
        AUTH_STATUS.UNAVAILABLE
    );
});

test("只有明确的 403 错误会被识别为未登录", () => {
    assert.equal(
        classifyAuthenticationError({response: {status: 403}}),
        AUTH_STATUS.UNAUTHENTICATED
    );
    assert.equal(
        classifyAuthenticationError({response: {status: 500}}),
        AUTH_STATUS.UNAVAILABLE
    );
    assert.equal(
        classifyAuthenticationError(new Error("Network Error")),
        AUTH_STATUS.UNAVAILABLE
    );
});

test("并发认证刷新只执行一次请求并返回同一个结果", async () => {
    let requestCount = 0;
    let resolveRequest;
    const request = () => {
        requestCount += 1;
        return new Promise((resolve) => {
            resolveRequest = resolve;
        });
    };

    const first = runSharedAuthRefresh(request);
    const second = runSharedAuthRefresh(request);

    assert.strictEqual(first, second);
    await Promise.resolve();
    assert.equal(requestCount, 1);

    resolveRequest(AUTH_STATUS.AUTHENTICATED);
    assert.equal(await first, AUTH_STATUS.AUTHENTICATED);
    assert.equal(await second, AUTH_STATUS.AUTHENTICATED);

    const third = runSharedAuthRefresh(async () => {
        requestCount += 1;
        return AUTH_STATUS.UNAUTHENTICATED;
    });
    assert.equal(await third, AUTH_STATUS.UNAUTHENTICATED);
    assert.equal(requestCount, 2);
});
