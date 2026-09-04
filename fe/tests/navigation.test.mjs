import assert from "node:assert/strict";
import test from "node:test";

import {
    createLoginRedirectHandler,
    getCurrentLoginRedirect,
    getSafeLoginRedirect,
    createSetupLocation
} from "../src/utils/navigation.mjs";

test("登录重定向只接受非登录页的站内路径", () => {
    assert.equal(getSafeLoginRedirect("/list?tag=inbox"), "/list?tag=inbox");
    assert.equal(getSafeLoginRedirect("https://example.com"), "/");
    assert.equal(getSafeLoginRedirect("//example.com"), "/");
    assert.equal(getSafeLoginRedirect("/%2fexample.com"), "/");
    assert.equal(getSafeLoginRedirect("/login?redirect=/list"), "/");
    assert.equal(getSafeLoginRedirect("/%6Cogin-verification"), "/");
    assert.equal(getSafeLoginRedirect("/login/extra"), "/");
});

test("已在登录页时保留原有安全重定向", () => {
    assert.equal(getCurrentLoginRedirect({
        name: "login",
        fullPath: "/login?redirect=/list",
        query: {redirect: "/list"}
    }), "/list");

    assert.equal(getCurrentLoginRedirect({
        name: "login",
        fullPath: "/login?redirect=/login",
        query: {redirect: "/login"}
    }), "/");
});

test("初始化跳转使用安全的站内重定向", () => {
    assert.deepEqual(createSetupLocation("/setup?redirect=/list"), {
        name: "setup",
        query: {redirect: "/"}
    });
    assert.deepEqual(createSetupLocation("/list?tag=inbox"), {
        name: "setup",
        query: {redirect: "/list?tag=inbox"}
    });
});

test("并发 403 只执行一次登录跳转且不会嵌套 redirect", async () => {
    let replaceCount = 0;
    let finishNavigation;
    const router = {
        currentRoute: {
            value: {
                name: "list",
                fullPath: "/list?tag=inbox",
                query: {}
            }
        },
        replace(location) {
            replaceCount += 1;
            return new Promise((resolve) => {
                finishNavigation = () => {
                    router.currentRoute.value = {
                        name: "login",
                        fullPath: "/login?redirect=/list?tag=inbox",
                        query: location.query
                    };
                    resolve();
                };
            });
        }
    };

    const redirectToLogin = createLoginRedirectHandler(router);
    const first = redirectToLogin();
    const second = redirectToLogin();

    assert.strictEqual(first, second);
    assert.equal(replaceCount, 1);
    finishNavigation();
    assert.equal(await first, true);
    assert.equal(router.currentRoute.value.query.redirect, "/list?tag=inbox");

    assert.equal(await redirectToLogin(), false);
    assert.equal(replaceCount, 1);
    assert.equal(router.currentRoute.value.query.redirect, "/list?tag=inbox");
});
