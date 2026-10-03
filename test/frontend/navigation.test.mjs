import assert from "node:assert/strict";
import test from "node:test";

import {
    createRouteAssetRecovery,
    isRouteAssetLoadError,
    createLoginRedirectHandler,
    getCurrentLoginRedirect,
    getSafeLoginRedirect,
    createSetupLocation
} from "../../frontend/src/utils/navigation.mjs";

test("登录重定向只接受非登录页的站内路径", () => {
    assert.equal(getSafeLoginRedirect("/list?tag=inbox"), "/list?tag=inbox");
    assert.equal(getSafeLoginRedirect("https://example.com"), "/");
    assert.equal(getSafeLoginRedirect("//example.com"), "/");
    assert.equal(getSafeLoginRedirect("/%2fexample.com"), "/");
    assert.equal(getSafeLoginRedirect("/login?redirect=/list"), "/");
    assert.equal(getSafeLoginRedirect("/%6Cogin-verification"), "/");
    assert.equal(getSafeLoginRedirect("/login/extra"), "/");
});

test('模块与样式加载错误才进入资源恢复流程', () => {
    for (const message of [
        'Unable to preload CSS for /assets/old.css',
        'Failed to fetch dynamically imported module: https://example.com/assets/old.js',
        'error loading dynamically imported module',
        'Importing a module script failed.'
    ]) assert.equal(isRouteAssetLoadError(new Error(message)), true);
    assert.equal(isRouteAssetLoadError(new Error('Network Error')), false);
    assert.equal(isRouteAssetLoadError(new Error('邮件发送失败')), false);
    assert.equal(isRouteAssetLoadError(null), false);
});

test('资源恢复保留目标路径且同一构建只自动刷新一次', () => {
    const values = new Map();
    const storage = {getItem: key => values.get(key), setItem: (key, value) => values.set(key, value)};
    const paths = [];
    const options = {buildID: 'release-a', getStorage: () => storage, reload: path => paths.push(path)};
    const recover = createRouteAssetRecovery(options);
    const error = new Error('Unable to preload CSS for /assets/old.css');
    const from = {name: 'list'};
    const to = {name: 'editer', fullPath: '/editer?draft=20'};
    assert.equal(recover(error, to, from), true);
    assert.equal(recover(error, to, from), true);
    assert.deepEqual(paths, ['/editer?draft=20']);
    assert.equal(createRouteAssetRecovery(options)(error, to, from), false);
    assert.equal(createRouteAssetRecovery({...options, buildID: 'release-b'})(error, to, from), true);
    assert.equal(paths.length, 2);
});

test('旧邮件页登录资源失效时恢复安全登录目标且不重复刷新', () => {
    for (const [redirect, expected] of [
        ['/detail/21', '/detail/21'],
        ['/list?tag=inbox', '/list?tag=inbox'],
        ['https://example.com', '/'],
        ['//example.com', '/'],
        ['/login?redirect=/list', '/'],
        [undefined, '/']
    ]) {
        const values = new Map();
        const paths = [];
        const options = {
            buildID: 'release-login',
            getStorage: () => ({getItem: key => values.get(key), setItem: (key, value) => values.set(key, value)}),
            reload: path => paths.push(path)
        };
        const error = new Error('Unable to preload CSS for /assets/Login-old.css');
        const to = {name: 'login', path: '/login', fullPath: '/login', query: {redirect}};
        const from = {name: 'list'};
        const recover = createRouteAssetRecovery(options);
        assert.equal(recover(error, to, from), true);
        assert.equal(recover(error, to, from), true);
        assert.deepEqual(paths, [`/login?${new URLSearchParams({redirect: expected})}`]);
        assert.equal(createRouteAssetRecovery(options)(error, to, from), false);
    }
});

test('登录资源恢复不刷新编辑表单、不恢复验证步骤或离线页面', () => {
    const recover = createRouteAssetRecovery({
        buildID: 'release-login',
        getStorage: () => assert.fail('不应访问存储'),
        reload: () => assert.fail('不应刷新')
    });
    const error = new Error('Unable to preload CSS for /assets/Login-old.css');
    const to = {name: 'login', path: '/login', fullPath: '/login', query: {redirect: '/list'}};
    assert.equal(recover(error, to, {name: 'editer'}), false);
    assert.equal(recover(error, to, {name: 'login'}), false);
    assert.equal(recover(error, to, {name: 'list'}, {editing: true}), false);
    assert.equal(recover(error, to, {name: 'list'}, {online: false}), false);
    assert.equal(recover(error, {...to, name: 'login-verification'}, {name: 'list'}), false);
    assert.equal(recover(error, {...to, path: '//example.com'}, {name: 'list'}), false);
});

test('编辑页、设置表单、离线和非法目标不会自动刷新', () => {
    let storageCalls = 0;
    const recover = createRouteAssetRecovery({
        buildID: 'release-a',
        getStorage: () => { storageCalls += 1; throw new Error('不应访问存储'); },
        reload: () => assert.fail('不应刷新')
    });
    const error = new Error('Unable to preload CSS for /assets/old.css');
    const to = {name: 'detail', fullPath: '/detail/21'};
    for (const name of ['editer', 'setup', 'login', 'force-password-change', undefined]) {
        assert.equal(recover(error, to, {name}), false);
    }
    assert.equal(recover(error, to, {name: 'list'}, {editing: true}), false);
    assert.equal(recover(error, to, {name: 'list'}, {online: false}), false);
    for (const fullPath of ['//example.com', '/%2fexample.com', 'https://example.com', '/login', '']) {
        assert.equal(recover(error, {...to, fullPath}, {name: 'list'}), false);
    }
    assert.equal(recover(new Error('Network Error'), to, {name: 'list'}), false);
    assert.equal(storageCalls, 0);
});

test('存储不可用或导航异常时交回手动处理', () => {
    const error = new Error('Unable to preload CSS for /assets/old.css');
    const to = {name: 'detail', fullPath: '/detail/21'};
    for (const getStorage of [
        () => { throw new Error('存储不可用'); },
        () => ({getItem: () => null, setItem: () => { throw new Error('写入失败'); }})
    ]) {
        const recover = createRouteAssetRecovery({buildID: 'release-a', getStorage, reload: () => assert.fail('不应刷新')});
        assert.equal(recover(error, to, {name: 'list'}), false);
    }
    const recover = createRouteAssetRecovery({
        buildID: 'release-a',
        getStorage: () => ({getItem: () => null, setItem: () => {}}),
        reload: () => { throw new Error('导航失败'); }
    });
    assert.equal(recover(error, to, {name: 'list'}), false);
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
