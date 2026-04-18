(function webpackUniversalModuleDefinition(root, factory) {
	if(typeof exports === 'object' && typeof module === 'object')
		module.exports = factory();
	else if(typeof define === 'function' && define.amd)
		define([], factory);
	else if(typeof exports === 'object')
		exports["mpegts"] = factory();
	else
		root["mpegts"] = factory();
})(window, function() {
return /******/ (function(modules) { // webpackBootstrap
/******/ 	// The module cache
/******/ 	var installedModules = {};
/******/
/******/ 	// The require function
/******/ 	function __webpack_require__(moduleId) {
/******/
/******/ 		// Check if module is in cache
/******/ 		if(installedModules[moduleId]) {
/******/ 			return installedModules[moduleId].exports;
/******/ 		}
/******/ 		// Create a new module (and put it into the cache)
/******/ 		var module = installedModules[moduleId] = {
/******/ 			i: moduleId,
/******/ 			l: false,
/******/ 			exports: {}
/******/ 		};
/******/
/******/ 		// Execute the module function
/******/ 		modules[moduleId].call(module.exports, module, module.exports, __webpack_require__);
/******/
/******/ 		// Flag the module as loaded
/******/ 		module.l = true;
/******/
/******/ 		// Return the exports of the module
/******/ 		return module.exports;
/******/ 	}
/******/
/******/
/******/ 	// expose the modules object (__webpack_modules__)
/******/ 	__webpack_require__.m = modules;
/******/
/******/ 	// expose the module cache
/******/ 	__webpack_require__.c = installedModules;
/******/
/******/ 	// define getter function for harmony exports
/******/ 	__webpack_require__.d = function(exports, name, getter) {
/******/ 		if(!__webpack_require__.o(exports, name)) {
/******/ 			Object.defineProperty(exports, name, { enumerable: true, get: getter });
/******/ 		}
/******/ 	};
/******/
/******/ 	// define __esModule on exports
/******/ 	__webpack_require__.r = function(exports) {
/******/ 		if(typeof Symbol !== 'undefined' && Symbol.toStringTag) {
/******/ 			Object.defineProperty(exports, Symbol.toStringTag, { value: 'Module' });
/******/ 		}
/******/ 		Object.defineProperty(exports, '__esModule', { value: true });
/******/ 	};
/******/
/******/ 	// create a fake namespace object
/******/ 	// mode & 1: value is a module id, require it
/******/ 	// mode & 2: merge all properties of value into the ns
/******/ 	// mode & 4: return value when already ns object
/******/ 	// mode & 8|1: behave like require
/******/ 	__webpack_require__.t = function(value, mode) {
/******/ 		if(mode & 1) value = __webpack_require__(value);
/******/ 		if(mode & 8) return value;
/******/ 		if((mode & 4) && typeof value === 'object' && value && value.__esModule) return value;
/******/ 		var ns = Object.create(null);
/******/ 		__webpack_require__.r(ns);
/******/ 		Object.defineProperty(ns, 'default', { enumerable: true, value: value });
/******/ 		if(mode & 2 && typeof value != 'string') for(var key in value) __webpack_require__.d(ns, key, function(key) { return value[key]; }.bind(null, key));
/******/ 		return ns;
/******/ 	};
/******/
/******/ 	// getDefaultExport function for compatibility with non-harmony modules
/******/ 	__webpack_require__.n = function(module) {
/******/ 		var getter = module && module.__esModule ?
/******/ 			function getDefault() { return module['default']; } :
/******/ 			function getModuleExports() { return module; };
/******/ 		__webpack_require__.d(getter, 'a', getter);
/******/ 		return getter;
/******/ 	};
/******/
/******/ 	// Object.prototype.hasOwnProperty.call
/******/ 	__webpack_require__.o = function(object, property) { return Object.prototype.hasOwnProperty.call(object, property); };
/******/
/******/ 	// __webpack_public_path__
/******/ 	__webpack_require__.p = "";
/******/
/******/
/******/ 	// Load entry module and return exports
/******/ 	return __webpack_require__(__webpack_require__.s = "./src/index.js");
/******/ })
/************************************************************************/
/******/ ({

/***/ "./node_modules/base64-js/index.js":
/*!*****************************************!*\
  !*** ./node_modules/base64-js/index.js ***!
  \*****************************************/
/*! no static exports found */
/***/ (function(module, exports, __webpack_require__) {

"use strict";

exports.byteLength = byteLength;
exports.toByteArray = toByteArray;
exports.fromByteArray = fromByteArray;
var lookup = [];
var revLookup = [];
var Arr = typeof Uint8Array !== 'undefined' ? Uint8Array : Array;
var code = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
for (var i = 0, len = code.length; i < len; ++i) {
    lookup[i] = code[i];
    revLookup[code.charCodeAt(i)] = i;
}
// Support decoding URL-safe base64 strings, as Node.js does.
// See: https://en.wikipedia.org/wiki/Base64#URL_applications
revLookup['-'.charCodeAt(0)] = 62;
revLookup['_'.charCodeAt(0)] = 63;
function getLens(b64) {
    var len = b64.length;
    if (len % 4 > 0) {
        throw new Error('Invalid string. Length must be a multiple of 4');
    }
    // Trim off extra bytes after placeholder bytes are found
    // See: https://github.com/beatgammit/base64-js/issues/42
    var validLen = b64.indexOf('=');
    if (validLen === -1)
        validLen = len;
    var placeHoldersLen = validLen === len
        ? 0
        : 4 - (validLen % 4);
    return [validLen, placeHoldersLen];
}
// base64 is 4/3 + up to two characters of the original data
function byteLength(b64) {
    var lens = getLens(b64);
    var validLen = lens[0];
    var placeHoldersLen = lens[1];
    return ((validLen + placeHoldersLen) * 3 / 4) - placeHoldersLen;
}
function _byteLength(b64, validLen, placeHoldersLen) {
    return ((validLen + placeHoldersLen) * 3 / 4) - placeHoldersLen;
}
function toByteArray(b64) {
    var tmp;
    var lens = getLens(b64);
    var validLen = lens[0];
    var placeHoldersLen = lens[1];
    var arr = new Arr(_byteLength(b64, validLen, placeHoldersLen));
    var curByte = 0;
    // if there are placeholders, only get up to the last complete 4 chars
    var len = placeHoldersLen > 0
        ? validLen - 4
        : validLen;
    var i;
    for (i = 0; i < len; i += 4) {
        tmp =
            (revLookup[b64.charCodeAt(i)] << 18) |
                (revLookup[b64.charCodeAt(i + 1)] << 12) |
                (revLookup[b64.charCodeAt(i + 2)] << 6) |
                revLookup[b64.charCodeAt(i + 3)];
        arr[curByte++] = (tmp >> 16) & 0xFF;
        arr[curByte++] = (tmp >> 8) & 0xFF;
        arr[curByte++] = tmp & 0xFF;
    }
    if (placeHoldersLen === 2) {
        tmp =
            (revLookup[b64.charCodeAt(i)] << 2) |
                (revLookup[b64.charCodeAt(i + 1)] >> 4);
        arr[curByte++] = tmp & 0xFF;
    }
    if (placeHoldersLen === 1) {
        tmp =
            (revLookup[b64.charCodeAt(i)] << 10) |
                (revLookup[b64.charCodeAt(i + 1)] << 4) |
                (revLookup[b64.charCodeAt(i + 2)] >> 2);
        arr[curByte++] = (tmp >> 8) & 0xFF;
        arr[curByte++] = tmp & 0xFF;
    }
    return arr;
}
function tripletToBase64(num) {
    return lookup[num >> 18 & 0x3F] +
        lookup[num >> 12 & 0x3F] +
        lookup[num >> 6 & 0x3F] +
        lookup[num & 0x3F];
}
function encodeChunk(uint8, start, end) {
    var tmp;
    var output = [];
    for (var i = start; i < end; i += 3) {
        tmp =
            ((uint8[i] << 16) & 0xFF0000) +
                ((uint8[i + 1] << 8) & 0xFF00) +
                (uint8[i + 2] & 0xFF);
        output.push(tripletToBase64(tmp));
    }
    return output.join('');
}
function fromByteArray(uint8) {
    var tmp;
    var len = uint8.length;
    var extraBytes = len % 3; // if we have 1 byte left, pad 2 bytes
    var parts = [];
    var maxChunkLength = 16383; // must be multiple of 3
    // go through the array every three bytes, we'll deal with trailing stuff later
    for (var i = 0, len2 = len - extraBytes; i < len2; i += maxChunkLength) {
        parts.push(encodeChunk(uint8, i, (i + maxChunkLength) > len2 ? len2 : (i + maxChunkLength)));
    }
    // pad the end with zeros, but make sure to not forget the extra bytes
    if (extraBytes === 1) {
        tmp = uint8[len - 1];
        parts.push(lookup[tmp >> 2] +
            lookup[(tmp << 4) & 0x3F] +
            '==');
    }
    else if (extraBytes === 2) {
        tmp = (uint8[len - 2] << 8) + uint8[len - 1];
        parts.push(lookup[tmp >> 10] +
            lookup[(tmp >> 4) & 0x3F] +
            lookup[(tmp << 2) & 0x3F] +
            '=');
    }
    return parts.join('');
}


/***/ }),

/***/ "./node_modules/buffer/index.js":
/*!**************************************!*\
  !*** ./node_modules/buffer/index.js ***!
  \**************************************/
/*! no static exports found */
/***/ (function(module, exports, __webpack_require__) {

"use strict";
/* WEBPACK VAR INJECTION */(function(global) {/*!
 * The buffer module from node.js, for the browser.
 *
 * @author   Feross Aboukhadijeh <http://feross.org>
 * @license  MIT
 */
/* eslint-disable no-proto */

var base64 = __webpack_require__(/*! base64-js */ "./node_modules/base64-js/index.js");
var ieee754 = __webpack_require__(/*! ieee754 */ "./node_modules/ieee754/index.js");
var isArray = __webpack_require__(/*! isarray */ "./node_modules/buffer/node_modules/isarray/index.js");
exports.Buffer = Buffer;
exports.SlowBuffer = SlowBuffer;
exports.INSPECT_MAX_BYTES = 50;
/**
 * If `Buffer.TYPED_ARRAY_SUPPORT`:
 *   === true    Use Uint8Array implementation (fastest)
 *   === false   Use Object implementation (most compatible, even IE6)
 *
 * Browsers that support typed arrays are IE 10+, Firefox 4+, Chrome 7+, Safari 5.1+,
 * Opera 11.6+, iOS 4.2+.
 *
 * Due to various browser bugs, sometimes the Object implementation will be used even
 * when the browser supports typed arrays.
 *
 * Note:
 *
 *   - Firefox 4-29 lacks support for adding new properties to `Uint8Array` instances,
 *     See: https://bugzilla.mozilla.org/show_bug.cgi?id=695438.
 *
 *   - Chrome 9-10 is missing the `TypedArray.prototype.subarray` function.
 *
 *   - IE10 has a broken `TypedArray.prototype.subarray` function which returns arrays of
 *     incorrect length in some situations.

 * We detect these buggy browsers and set `Buffer.TYPED_ARRAY_SUPPORT` to `false` so they
 * get the Object implementation, which is slower but behaves correctly.
 */
Buffer.TYPED_ARRAY_SUPPORT = global.TYPED_ARRAY_SUPPORT !== undefined
    ? global.TYPED_ARRAY_SUPPORT
    : typedArraySupport();
/*
 * Export kMaxLength after typed array support is determined.
 */
exports.kMaxLength = kMaxLength();
function typedArraySupport() {
    try {
        var arr = new Uint8Array(1);
        arr.__proto__ = { __proto__: Uint8Array.prototype, foo: function () { return 42; } };
        return arr.foo() === 42 && // typed array instances can be augmented
            typeof arr.subarray === 'function' && // chrome 9-10 lack `subarray`
            arr.subarray(1, 1).byteLength === 0; // ie10 has broken `subarray`
    }
    catch (e) {
        return false;
    }
}
function kMaxLength() {
    return Buffer.TYPED_ARRAY_SUPPORT
        ? 0x7fffffff
        : 0x3fffffff;
}
function createBuffer(that, length) {
    if (kMaxLength() < length) {
        throw new RangeError('Invalid typed array length');
    }
    if (Buffer.TYPED_ARRAY_SUPPORT) {
        // Return an augmented `Uint8Array` instance, for best performance
        that = new Uint8Array(length);
        that.__proto__ = Buffer.prototype;
    }
    else {
        // Fallback: Return an object instance of the Buffer class
        if (that === null) {
            that = new Buffer(length);
        }
        that.length = length;
    }
    return that;
}
/**
 * The Buffer constructor returns instances of `Uint8Array` that have their
 * prototype changed to `Buffer.prototype`. Furthermore, `Buffer` is a subclass of
 * `Uint8Array`, so the returned instances will have all the node `Buffer` methods
 * and the `Uint8Array` methods. Square bracket notation works as expected -- it
 * returns a single octet.
 *
 * The `Uint8Array` prototype remains unmodified.
 */
function Buffer(arg, encodingOrOffset, length) {
    if (!Buffer.TYPED_ARRAY_SUPPORT && !(this instanceof Buffer)) {
        return new Buffer(arg, encodingOrOffset, length);
    }
    // Common case.
    if (typeof arg === 'number') {
        if (typeof encodingOrOffset === 'string') {
            throw new Error('If encoding is specified then the first argument must be a string');
        }
        return allocUnsafe(this, arg);
    }
    return from(this, arg, encodingOrOffset, length);
}
Buffer.poolSize = 8192; // not used by this implementation
// TODO: Legacy, not needed anymore. Remove in next major version.
Buffer._augment = function (arr) {
    arr.__proto__ = Buffer.prototype;
    return arr;
};
function from(that, value, encodingOrOffset, length) {
    if (typeof value === 'number') {
        throw new TypeError('"value" argument must not be a number');
    }
    if (typeof ArrayBuffer !== 'undefined' && value instanceof ArrayBuffer) {
        return fromArrayBuffer(that, value, encodingOrOffset, length);
    }
    if (typeof value === 'string') {
        return fromString(that, value, encodingOrOffset);
    }
    return fromObject(that, value);
}
/**
 * Functionally equivalent to Buffer(arg, encoding) but throws a TypeError
 * if value is a number.
 * Buffer.from(str[, encoding])
 * Buffer.from(array)
 * Buffer.from(buffer)
 * Buffer.from(arrayBuffer[, byteOffset[, length]])
 **/
Buffer.from = function (value, encodingOrOffset, length) {
    return from(null, value, encodingOrOffset, length);
};
if (Buffer.TYPED_ARRAY_SUPPORT) {
    Buffer.prototype.__proto__ = Uint8Array.prototype;
    Buffer.__proto__ = Uint8Array;
    if (typeof Symbol !== 'undefined' && Symbol.species &&
        Buffer[Symbol.species] === Buffer) {
        // Fix subarray() in ES2016. See: https://github.com/feross/buffer/pull/97
        Object.defineProperty(Buffer, Symbol.species, {
            value: null,
            configurable: true
        });
    }
}
function assertSize(size) {
    if (typeof size !== 'number') {
        throw new TypeError('"size" argument must be a number');
    }
    else if (size < 0) {
        throw new RangeError('"size" argument must not be negative');
    }
}
function alloc(that, size, fill, encoding) {
    assertSize(size);
    if (size <= 0) {
        return createBuffer(that, size);
    }
    if (fill !== undefined) {
        // Only pay attention to encoding if it's a string. This
        // prevents accidentally sending in a number that would
        // be interpretted as a start offset.
        return typeof encoding === 'string'
            ? createBuffer(that, size).fill(fill, encoding)
            : createBuffer(that, size).fill(fill);
    }
    return createBuffer(that, size);
}
/**
 * Creates a new filled Buffer instance.
 * alloc(size[, fill[, encoding]])
 **/
Buffer.alloc = function (size, fill, encoding) {
    return alloc(null, size, fill, encoding);
};
function allocUnsafe(that, size) {
    assertSize(size);
    that = createBuffer(that, size < 0 ? 0 : checked(size) | 0);
    if (!Buffer.TYPED_ARRAY_SUPPORT) {
        for (var i = 0; i < size; ++i) {
            that[i] = 0;
        }
    }
    return that;
}
/**
 * Equivalent to Buffer(num), by default creates a non-zero-filled Buffer instance.
 * */
Buffer.allocUnsafe = function (size) {
    return allocUnsafe(null, size);
};
/**
 * Equivalent to SlowBuffer(num), by default creates a non-zero-filled Buffer instance.
 */
Buffer.allocUnsafeSlow = function (size) {
    return allocUnsafe(null, size);
};
function fromString(that, string, encoding) {
    if (typeof encoding !== 'string' || encoding === '') {
        encoding = 'utf8';
    }
    if (!Buffer.isEncoding(encoding)) {
        throw new TypeError('"encoding" must be a valid string encoding');
    }
    var length = byteLength(string, encoding) | 0;
    that = createBuffer(that, length);
    var actual = that.write(string, encoding);
    if (actual !== length) {
        // Writing a hex string, for example, that contains invalid characters will
        // cause everything after the first invalid character to be ignored. (e.g.
        // 'abxxcd' will be treated as 'ab')
        that = that.slice(0, actual);
    }
    return that;
}
function fromArrayLike(that, array) {
    var length = array.length < 0 ? 0 : checked(array.length) | 0;
    that = createBuffer(that, length);
    for (var i = 0; i < length; i += 1) {
        that[i] = array[i] & 255;
    }
    return that;
}
function fromArrayBuffer(that, array, byteOffset, length) {
    array.byteLength; // this throws if `array` is not a valid ArrayBuffer
    if (byteOffset < 0 || array.byteLength < byteOffset) {
        throw new RangeError('\'offset\' is out of bounds');
    }
    if (array.byteLength < byteOffset + (length || 0)) {
        throw new RangeError('\'length\' is out of bounds');
    }
    if (byteOffset === undefined && length === undefined) {
        array = new Uint8Array(array);
    }
    else if (length === undefined) {
        array = new Uint8Array(array, byteOffset);
    }
    else {
        array = new Uint8Array(array, byteOffset, length);
    }
    if (Buffer.TYPED_ARRAY_SUPPORT) {
        // Return an augmented `Uint8Array` instance, for best performance
        that = array;
        that.__proto__ = Buffer.prototype;
    }
    else {
        // Fallback: Return an object instance of the Buffer class
        that = fromArrayLike(that, array);
    }
    return that;
}
function fromObject(that, obj) {
    if (Buffer.isBuffer(obj)) {
        var len = checked(obj.length) | 0;
        that = createBuffer(that, len);
        if (that.length === 0) {
            return that;
        }
        obj.copy(that, 0, 0, len);
        return that;
    }
    if (obj) {
        if ((typeof ArrayBuffer !== 'undefined' &&
            obj.buffer instanceof ArrayBuffer) || 'length' in obj) {
            if (typeof obj.length !== 'number' || isnan(obj.length)) {
                return createBuffer(that, 0);
            }
            return fromArrayLike(that, obj);
        }
        if (obj.type === 'Buffer' && isArray(obj.data)) {
            return fromArrayLike(that, obj.data);
        }
    }
    throw new TypeError('First argument must be a string, Buffer, ArrayBuffer, Array, or array-like object.');
}
function checked(length) {
    // Note: cannot use `length < kMaxLength()` here because that fails when
    // length is NaN (which is otherwise coerced to zero.)
    if (length >= kMaxLength()) {
        throw new RangeError('Attempt to allocate Buffer larger than maximum ' +
            'size: 0x' + kMaxLength().toString(16) + ' bytes');
    }
    return length | 0;
}
function SlowBuffer(length) {
    if (+length != length) { // eslint-disable-line eqeqeq
        length = 0;
    }
    return Buffer.alloc(+length);
}
Buffer.isBuffer = function isBuffer(b) {
    return !!(b != null && b._isBuffer);
};
Buffer.compare = function compare(a, b) {
    if (!Buffer.isBuffer(a) || !Buffer.isBuffer(b)) {
        throw new TypeError('Arguments must be Buffers');
    }
    if (a === b)
        return 0;
    var x = a.length;
    var y = b.length;
    for (var i = 0, len = Math.min(x, y); i < len; ++i) {
        if (a[i] !== b[i]) {
            x = a[i];
            y = b[i];
            break;
        }
    }
    if (x < y)
        return -1;
    if (y < x)
        return 1;
    return 0;
};
Buffer.isEncoding = function isEncoding(encoding) {
    switch (String(encoding).toLowerCase()) {
        case 'hex':
        case 'utf8':
        case 'utf-8':
        case 'ascii':
        case 'latin1':
        case 'binary':
        case 'base64':
        case 'ucs2':
        case 'ucs-2':
        case 'utf16le':
        case 'utf-16le':
            return true;
        default:
            return false;
    }
};
Buffer.concat = function concat(list, length) {
    if (!isArray(list)) {
        throw new TypeError('"list" argument must be an Array of Buffers');
    }
    if (list.length === 0) {
        return Buffer.alloc(0);
    }
    var i;
    if (length === undefined) {
        length = 0;
        for (i = 0; i < list.length; ++i) {
            length += list[i].length;
        }
    }
    var buffer = Buffer.allocUnsafe(length);
    var pos = 0;
    for (i = 0; i < list.length; ++i) {
        var buf = list[i];
        if (!Buffer.isBuffer(buf)) {
            throw new TypeError('"list" argument must be an Array of Buffers');
        }
        buf.copy(buffer, pos);
        pos += buf.length;
    }
    return buffer;
};
function byteLength(string, encoding) {
    if (Buffer.isBuffer(string)) {
        return string.length;
    }
    if (typeof ArrayBuffer !== 'undefined' && typeof ArrayBuffer.isView === 'function' &&
        (ArrayBuffer.isView(string) || string instanceof ArrayBuffer)) {
        return string.byteLength;
    }
    if (typeof string !== 'string') {
        string = '' + string;
    }
    var len = string.length;
    if (len === 0)
        return 0;
    // Use a for loop to avoid recursion
    var loweredCase = false;
    for (;;) {
        switch (encoding) {
            case 'ascii':
            case 'latin1':
            case 'binary':
                return len;
            case 'utf8':
            case 'utf-8':
            case undefined:
                return utf8ToBytes(string).length;
            case 'ucs2':
            case 'ucs-2':
            case 'utf16le':
            case 'utf-16le':
                return len * 2;
            case 'hex':
                return len >>> 1;
            case 'base64':
                return base64ToBytes(string).length;
            default:
                if (loweredCase)
                    return utf8ToBytes(string).length; // assume utf8
                encoding = ('' + encoding).toLowerCase();
                loweredCase = true;
        }
    }
}
Buffer.byteLength = byteLength;
function slowToString(encoding, start, end) {
    var loweredCase = false;
    // No need to verify that "this.length <= MAX_UINT32" since it's a read-only
    // property of a typed array.
    // This behaves neither like String nor Uint8Array in that we set start/end
    // to their upper/lower bounds if the value passed is out of range.
    // undefined is handled specially as per ECMA-262 6th Edition,
    // Section 13.3.3.7 Runtime Semantics: KeyedBindingInitialization.
    if (start === undefined || start < 0) {
        start = 0;
    }
    // Return early if start > this.length. Done here to prevent potential uint32
    // coercion fail below.
    if (start > this.length) {
        return '';
    }
    if (end === undefined || end > this.length) {
        end = this.length;
    }
    if (end <= 0) {
        return '';
    }
    // Force coersion to uint32. This will also coerce falsey/NaN values to 0.
    end >>>= 0;
    start >>>= 0;
    if (end <= start) {
        return '';
    }
    if (!encoding)
        encoding = 'utf8';
    while (true) {
        switch (encoding) {
            case 'hex':
                return hexSlice(this, start, end);
            case 'utf8':
            case 'utf-8':
                return utf8Slice(this, start, end);
            case 'ascii':
                return asciiSlice(this, start, end);
            case 'latin1':
            case 'binary':
                return latin1Slice(this, start, end);
            case 'base64':
                return base64Slice(this, start, end);
            case 'ucs2':
            case 'ucs-2':
            case 'utf16le':
            case 'utf-16le':
                return utf16leSlice(this, start, end);
            default:
                if (loweredCase)
                    throw new TypeError('Unknown encoding: ' + encoding);
                encoding = (encoding + '').toLowerCase();
                loweredCase = true;
        }
    }
}
// The property is used by `Buffer.isBuffer` and `is-buffer` (in Safari 5-7) to detect
// Buffer instances.
Buffer.prototype._isBuffer = true;
function swap(b, n, m) {
    var i = b[n];
    b[n] = b[m];
    b[m] = i;
}
Buffer.prototype.swap16 = function swap16() {
    var len = this.length;
    if (len % 2 !== 0) {
        throw new RangeError('Buffer size must be a multiple of 16-bits');
    }
    for (var i = 0; i < len; i += 2) {
        swap(this, i, i + 1);
    }
    return this;
};
Buffer.prototype.swap32 = function swap32() {
    var len = this.length;
    if (len % 4 !== 0) {
        throw new RangeError('Buffer size must be a multiple of 32-bits');
    }
    for (var i = 0; i < len; i += 4) {
        swap(this, i, i + 3);
        swap(this, i + 1, i + 2);
    }
    return this;
};
Buffer.prototype.swap64 = function swap64() {
    var len = this.length;
    if (len % 8 !== 0) {
        throw new RangeError('Buffer size must be a multiple of 64-bits');
    }
    for (var i = 0; i < len; i += 8) {
        swap(this, i, i + 7);
        swap(this, i + 1, i + 6);
        swap(this, i + 2, i + 5);
        swap(this, i + 3, i + 4);
    }
    return this;
};
Buffer.prototype.toString = function toString() {
    var length = this.length | 0;
    if (length === 0)
        return '';
    if (arguments.length === 0)
        return utf8Slice(this, 0, length);
    return slowToString.apply(this, arguments);
};
Buffer.prototype.equals = function equals(b) {
    if (!Buffer.isBuffer(b))
        throw new TypeError('Argument must be a Buffer');
    if (this === b)
        return true;
    return Buffer.compare(this, b) === 0;
};
Buffer.prototype.inspect = function inspect() {
    var str = '';
    var max = exports.INSPECT_MAX_BYTES;
    if (this.length > 0) {
        str = this.toString('hex', 0, max).match(/.{2}/g).join(' ');
        if (this.length > max)
            str += ' ... ';
    }
    return '<Buffer ' + str + '>';
};
Buffer.prototype.compare = function compare(target, start, end, thisStart, thisEnd) {
    if (!Buffer.isBuffer(target)) {
        throw new TypeError('Argument must be a Buffer');
    }
    if (start === undefined) {
        start = 0;
    }
    if (end === undefined) {
        end = target ? target.length : 0;
    }
    if (thisStart === undefined) {
        thisStart = 0;
    }
    if (thisEnd === undefined) {
        thisEnd = this.length;
    }
    if (start < 0 || end > target.length || thisStart < 0 || thisEnd > this.length) {
        throw new RangeError('out of range index');
    }
    if (thisStart >= thisEnd && start >= end) {
        return 0;
    }
    if (thisStart >= thisEnd) {
        return -1;
    }
    if (start >= end) {
        return 1;
    }
    start >>>= 0;
    end >>>= 0;
    thisStart >>>= 0;
    thisEnd >>>= 0;
    if (this === target)
        return 0;
    var x = thisEnd - thisStart;
    var y = end - start;
    var len = Math.min(x, y);
    var thisCopy = this.slice(thisStart, thisEnd);
    var targetCopy = target.slice(start, end);
    for (var i = 0; i < len; ++i) {
        if (thisCopy[i] !== targetCopy[i]) {
            x = thisCopy[i];
            y = targetCopy[i];
            break;
        }
    }
    if (x < y)
        return -1;
    if (y < x)
        return 1;
    return 0;
};
// Finds either the first index of `val` in `buffer` at offset >= `byteOffset`,
// OR the last index of `val` in `buffer` at offset <= `byteOffset`.
//
// Arguments:
// - buffer - a Buffer to search
// - val - a string, Buffer, or number
// - byteOffset - an index into `buffer`; will be clamped to an int32
// - encoding - an optional encoding, relevant is val is a string
// - dir - true for indexOf, false for lastIndexOf
function bidirectionalIndexOf(buffer, val, byteOffset, encoding, dir) {
    // Empty buffer means no match
    if (buffer.length === 0)
        return -1;
    // Normalize byteOffset
    if (typeof byteOffset === 'string') {
        encoding = byteOffset;
        byteOffset = 0;
    }
    else if (byteOffset > 0x7fffffff) {
        byteOffset = 0x7fffffff;
    }
    else if (byteOffset < -0x80000000) {
        byteOffset = -0x80000000;
    }
    byteOffset = +byteOffset; // Coerce to Number.
    if (isNaN(byteOffset)) {
        // byteOffset: it it's undefined, null, NaN, "foo", etc, search whole buffer
        byteOffset = dir ? 0 : (buffer.length - 1);
    }
    // Normalize byteOffset: negative offsets start from the end of the buffer
    if (byteOffset < 0)
        byteOffset = buffer.length + byteOffset;
    if (byteOffset >= buffer.length) {
        if (dir)
            return -1;
        else
            byteOffset = buffer.length - 1;
    }
    else if (byteOffset < 0) {
        if (dir)
            byteOffset = 0;
        else
            return -1;
    }
    // Normalize val
    if (typeof val === 'string') {
        val = Buffer.from(val, encoding);
    }
    // Finally, search either indexOf (if dir is true) or lastIndexOf
    if (Buffer.isBuffer(val)) {
        // Special case: looking for empty string/buffer always fails
        if (val.length === 0) {
            return -1;
        }
        return arrayIndexOf(buffer, val, byteOffset, encoding, dir);
    }
    else if (typeof val === 'number') {
        val = val & 0xFF; // Search for a byte value [0-255]
        if (Buffer.TYPED_ARRAY_SUPPORT &&
            typeof Uint8Array.prototype.indexOf === 'function') {
            if (dir) {
                return Uint8Array.prototype.indexOf.call(buffer, val, byteOffset);
            }
            else {
                return Uint8Array.prototype.lastIndexOf.call(buffer, val, byteOffset);
            }
        }
        return arrayIndexOf(buffer, [val], byteOffset, encoding, dir);
    }
    throw new TypeError('val must be string, number or Buffer');
}
function arrayIndexOf(arr, val, byteOffset, encoding, dir) {
    var indexSize = 1;
    var arrLength = arr.length;
    var valLength = val.length;
    if (encoding !== undefined) {
        encoding = String(encoding).toLowerCase();
        if (encoding === 'ucs2' || encoding === 'ucs-2' ||
            encoding === 'utf16le' || encoding === 'utf-16le') {
            if (arr.length < 2 || val.length < 2) {
                return -1;
            }
            indexSize = 2;
            arrLength /= 2;
            valLength /= 2;
            byteOffset /= 2;
        }
    }
    function read(buf, i) {
        if (indexSize === 1) {
            return buf[i];
        }
        else {
            return buf.readUInt16BE(i * indexSize);
        }
    }
    var i;
    if (dir) {
        var foundIndex = -1;
        for (i = byteOffset; i < arrLength; i++) {
            if (read(arr, i) === read(val, foundIndex === -1 ? 0 : i - foundIndex)) {
                if (foundIndex === -1)
                    foundIndex = i;
                if (i - foundIndex + 1 === valLength)
                    return foundIndex * indexSize;
            }
            else {
                if (foundIndex !== -1)
                    i -= i - foundIndex;
                foundIndex = -1;
            }
        }
    }
    else {
        if (byteOffset + valLength > arrLength)
            byteOffset = arrLength - valLength;
        for (i = byteOffset; i >= 0; i--) {
            var found = true;
            for (var j = 0; j < valLength; j++) {
                if (read(arr, i + j) !== read(val, j)) {
                    found = false;
                    break;
                }
            }
            if (found)
                return i;
        }
    }
    return -1;
}
Buffer.prototype.includes = function includes(val, byteOffset, encoding) {
    return this.indexOf(val, byteOffset, encoding) !== -1;
};
Buffer.prototype.indexOf = function indexOf(val, byteOffset, encoding) {
    return bidirectionalIndexOf(this, val, byteOffset, encoding, true);
};
Buffer.prototype.lastIndexOf = function lastIndexOf(val, byteOffset, encoding) {
    return bidirectionalIndexOf(this, val, byteOffset, encoding, false);
};
function hexWrite(buf, string, offset, length) {
    offset = Number(offset) || 0;
    var remaining = buf.length - offset;
    if (!length) {
        length = remaining;
    }
    else {
        length = Number(length);
        if (length > remaining) {
            length = remaining;
        }
    }
    // must be an even number of digits
    var strLen = string.length;
    if (strLen % 2 !== 0)
        throw new TypeError('Invalid hex string');
    if (length > strLen / 2) {
        length = strLen / 2;
    }
    for (var i = 0; i < length; ++i) {
        var parsed = parseInt(string.substr(i * 2, 2), 16);
        if (isNaN(parsed))
            return i;
        buf[offset + i] = parsed;
    }
    return i;
}
function utf8Write(buf, string, offset, length) {
    return blitBuffer(utf8ToBytes(string, buf.length - offset), buf, offset, length);
}
function asciiWrite(buf, string, offset, length) {
    return blitBuffer(asciiToBytes(string), buf, offset, length);
}
function latin1Write(buf, string, offset, length) {
    return asciiWrite(buf, string, offset, length);
}
function base64Write(buf, string, offset, length) {
    return blitBuffer(base64ToBytes(string), buf, offset, length);
}
function ucs2Write(buf, string, offset, length) {
    return blitBuffer(utf16leToBytes(string, buf.length - offset), buf, offset, length);
}
Buffer.prototype.write = function write(string, offset, length, encoding) {
    // Buffer#write(string)
    if (offset === undefined) {
        encoding = 'utf8';
        length = this.length;
        offset = 0;
        // Buffer#write(string, encoding)
    }
    else if (length === undefined && typeof offset === 'string') {
        encoding = offset;
        length = this.length;
        offset = 0;
        // Buffer#write(string, offset[, length][, encoding])
    }
    else if (isFinite(offset)) {
        offset = offset | 0;
        if (isFinite(length)) {
            length = length | 0;
            if (encoding === undefined)
                encoding = 'utf8';
        }
        else {
            encoding = length;
            length = undefined;
        }
        // legacy write(string, encoding, offset, length) - remove in v0.13
    }
    else {
        throw new Error('Buffer.write(string, encoding, offset[, length]) is no longer supported');
    }
    var remaining = this.length - offset;
    if (length === undefined || length > remaining)
        length = remaining;
    if ((string.length > 0 && (length < 0 || offset < 0)) || offset > this.length) {
        throw new RangeError('Attempt to write outside buffer bounds');
    }
    if (!encoding)
        encoding = 'utf8';
    var loweredCase = false;
    for (;;) {
        switch (encoding) {
            case 'hex':
                return hexWrite(this, string, offset, length);
            case 'utf8':
            case 'utf-8':
                return utf8Write(this, string, offset, length);
            case 'ascii':
                return asciiWrite(this, string, offset, length);
            case 'latin1':
            case 'binary':
                return latin1Write(this, string, offset, length);
            case 'base64':
                // Warning: maxLength not taken into account in base64Write
                return base64Write(this, string, offset, length);
            case 'ucs2':
            case 'ucs-2':
            case 'utf16le':
            case 'utf-16le':
                return ucs2Write(this, string, offset, length);
            default:
                if (loweredCase)
                    throw new TypeError('Unknown encoding: ' + encoding);
                encoding = ('' + encoding).toLowerCase();
                loweredCase = true;
        }
    }
};
Buffer.prototype.toJSON = function toJSON() {
    return {
        type: 'Buffer',
        data: Array.prototype.slice.call(this._arr || this, 0)
    };
};
function base64Slice(buf, start, end) {
    if (start === 0 && end === buf.length) {
        return base64.fromByteArray(buf);
    }
    else {
        return base64.fromByteArray(buf.slice(start, end));
    }
}
function utf8Slice(buf, start, end) {
    end = Math.min(buf.length, end);
    var res = [];
    var i = start;
    while (i < end) {
        var firstByte = buf[i];
        var codePoint = null;
        var bytesPerSequence = (firstByte > 0xEF) ? 4
            : (firstByte > 0xDF) ? 3
                : (firstByte > 0xBF) ? 2
                    : 1;
        if (i + bytesPerSequence <= end) {
            var secondByte, thirdByte, fourthByte, tempCodePoint;
            switch (bytesPerSequence) {
                case 1:
                    if (firstByte < 0x80) {
                        codePoint = firstByte;
                    }
                    break;
                case 2:
                    secondByte = buf[i + 1];
                    if ((secondByte & 0xC0) === 0x80) {
                        tempCodePoint = (firstByte & 0x1F) << 0x6 | (secondByte & 0x3F);
                        if (tempCodePoint > 0x7F) {
                            codePoint = tempCodePoint;
                        }
                    }
                    break;
                case 3:
                    secondByte = buf[i + 1];
                    thirdByte = buf[i + 2];
                    if ((secondByte & 0xC0) === 0x80 && (thirdByte & 0xC0) === 0x80) {
                        tempCodePoint = (firstByte & 0xF) << 0xC | (secondByte & 0x3F) << 0x6 | (thirdByte & 0x3F);
                        if (tempCodePoint > 0x7FF && (tempCodePoint < 0xD800 || tempCodePoint > 0xDFFF)) {
                            codePoint = tempCodePoint;
                        }
                    }
                    break;
                case 4:
                    secondByte = buf[i + 1];
                    thirdByte = buf[i + 2];
                    fourthByte = buf[i + 3];
                    if ((secondByte & 0xC0) === 0x80 && (thirdByte & 0xC0) === 0x80 && (fourthByte & 0xC0) === 0x80) {
                        tempCodePoint = (firstByte & 0xF) << 0x12 | (secondByte & 0x3F) << 0xC | (thirdByte & 0x3F) << 0x6 | (fourthByte & 0x3F);
                        if (tempCodePoint > 0xFFFF && tempCodePoint < 0x110000) {
                            codePoint = tempCodePoint;
                        }
                    }
            }
        }
        if (codePoint === null) {
            // we did not generate a valid codePoint so insert a
            // replacement char (U+FFFD) and advance only 1 byte
            codePoint = 0xFFFD;
            bytesPerSequence = 1;
        }
        else if (codePoint > 0xFFFF) {
            // encode to utf16 (surrogate pair dance)
            codePoint -= 0x10000;
            res.push(codePoint >>> 10 & 0x3FF | 0xD800);
            codePoint = 0xDC00 | codePoint & 0x3FF;
        }
        res.push(codePoint);
        i += bytesPerSequence;
    }
    return decodeCodePointsArray(res);
}
// Based on http://stackoverflow.com/a/22747272/680742, the browser with
// the lowest limit is Chrome, with 0x10000 args.
// We go 1 magnitude less, for safety
var MAX_ARGUMENTS_LENGTH = 0x1000;
function decodeCodePointsArray(codePoints) {
    var len = codePoints.length;
    if (len <= MAX_ARGUMENTS_LENGTH) {
        return String.fromCharCode.apply(String, codePoints); // avoid extra slice()
    }
    // Decode in chunks to avoid "call stack size exceeded".
    var res = '';
    var i = 0;
    while (i < len) {
        res += String.fromCharCode.apply(String, codePoints.slice(i, i += MAX_ARGUMENTS_LENGTH));
    }
    return res;
}
function asciiSlice(buf, start, end) {
    var ret = '';
    end = Math.min(buf.length, end);
    for (var i = start; i < end; ++i) {
        ret += String.fromCharCode(buf[i] & 0x7F);
    }
    return ret;
}
function latin1Slice(buf, start, end) {
    var ret = '';
    end = Math.min(buf.length, end);
    for (var i = start; i < end; ++i) {
        ret += String.fromCharCode(buf[i]);
    }
    return ret;
}
function hexSlice(buf, start, end) {
    var len = buf.length;
    if (!start || start < 0)
        start = 0;
    if (!end || end < 0 || end > len)
        end = len;
    var out = '';
    for (var i = start; i < end; ++i) {
        out += toHex(buf[i]);
    }
    return out;
}
function utf16leSlice(buf, start, end) {
    var bytes = buf.slice(start, end);
    var res = '';
    for (var i = 0; i < bytes.length; i += 2) {
        res += String.fromCharCode(bytes[i] + bytes[i + 1] * 256);
    }
    return res;
}
Buffer.prototype.slice = function slice(start, end) {
    var len = this.length;
    start = ~~start;
    end = end === undefined ? len : ~~end;
    if (start < 0) {
        start += len;
        if (start < 0)
            start = 0;
    }
    else if (start > len) {
        start = len;
    }
    if (end < 0) {
        end += len;
        if (end < 0)
            end = 0;
    }
    else if (end > len) {
        end = len;
    }
    if (end < start)
        end = start;
    var newBuf;
    if (Buffer.TYPED_ARRAY_SUPPORT) {
        newBuf = this.subarray(start, end);
        newBuf.__proto__ = Buffer.prototype;
    }
    else {
        var sliceLen = end - start;
        newBuf = new Buffer(sliceLen, undefined);
        for (var i = 0; i < sliceLen; ++i) {
            newBuf[i] = this[i + start];
        }
    }
    return newBuf;
};
/*
 * Need to make sure that buffer isn't trying to write out of bounds.
 */
function checkOffset(offset, ext, length) {
    if ((offset % 1) !== 0 || offset < 0)
        throw new RangeError('offset is not uint');
    if (offset + ext > length)
        throw new RangeError('Trying to access beyond buffer length');
}
Buffer.prototype.readUIntLE = function readUIntLE(offset, byteLength, noAssert) {
    offset = offset | 0;
    byteLength = byteLength | 0;
    if (!noAssert)
        checkOffset(offset, byteLength, this.length);
    var val = this[offset];
    var mul = 1;
    var i = 0;
    while (++i < byteLength && (mul *= 0x100)) {
        val += this[offset + i] * mul;
    }
    return val;
};
Buffer.prototype.readUIntBE = function readUIntBE(offset, byteLength, noAssert) {
    offset = offset | 0;
    byteLength = byteLength | 0;
    if (!noAssert) {
        checkOffset(offset, byteLength, this.length);
    }
    var val = this[offset + --byteLength];
    var mul = 1;
    while (byteLength > 0 && (mul *= 0x100)) {
        val += this[offset + --byteLength] * mul;
    }
    return val;
};
Buffer.prototype.readUInt8 = function readUInt8(offset, noAssert) {
    if (!noAssert)
        checkOffset(offset, 1, this.length);
    return this[offset];
};
Buffer.prototype.readUInt16LE = function readUInt16LE(offset, noAssert) {
    if (!noAssert)
        checkOffset(offset, 2, this.length);
    return this[offset] | (this[offset + 1] << 8);
};
Buffer.prototype.readUInt16BE = function readUInt16BE(offset, noAssert) {
    if (!noAssert)
        checkOffset(offset, 2, this.length);
    return (this[offset] << 8) | this[offset + 1];
};
Buffer.prototype.readUInt32LE = function readUInt32LE(offset, noAssert) {
    if (!noAssert)
        checkOffset(offset, 4, this.length);
    return ((this[offset]) |
        (this[offset + 1] << 8) |
        (this[offset + 2] << 16)) +
        (this[offset + 3] * 0x1000000);
};
Buffer.prototype.readUInt32BE = function readUInt32BE(offset, noAssert) {
    if (!noAssert)
        checkOffset(offset, 4, this.length);
    return (this[offset] * 0x1000000) +
        ((this[offset + 1] << 16) |
            (this[offset + 2] << 8) |
            this[offset + 3]);
};
Buffer.prototype.readIntLE = function readIntLE(offset, byteLength, noAssert) {
    offset = offset | 0;
    byteLength = byteLength | 0;
    if (!noAssert)
        checkOffset(offset, byteLength, this.length);
    var val = this[offset];
    var mul = 1;
    var i = 0;
    while (++i < byteLength && (mul *= 0x100)) {
        val += this[offset + i] * mul;
    }
    mul *= 0x80;
    if (val >= mul)
        val -= Math.pow(2, 8 * byteLength);
    return val;
};
Buffer.prototype.readIntBE = function readIntBE(offset, byteLength, noAssert) {
    offset = offset | 0;
    byteLength = byteLength | 0;
    if (!noAssert)
        checkOffset(offset, byteLength, this.length);
    var i = byteLength;
    var mul = 1;
    var val = this[offset + --i];
    while (i > 0 && (mul *= 0x100)) {
        val += this[offset + --i] * mul;
    }
    mul *= 0x80;
    if (val >= mul)
        val -= Math.pow(2, 8 * byteLength);
    return val;
};
Buffer.prototype.readInt8 = function readInt8(offset, noAssert) {
    if (!noAssert)
        checkOffset(offset, 1, this.length);
    if (!(this[offset] & 0x80))
        return (this[offset]);
    return ((0xff - this[offset] + 1) * -1);
};
Buffer.prototype.readInt16LE = function readInt16LE(offset, noAssert) {
    if (!noAssert)
        checkOffset(offset, 2, this.length);
    var val = this[offset] | (this[offset + 1] << 8);
    return (val & 0x8000) ? val | 0xFFFF0000 : val;
};
Buffer.prototype.readInt16BE = function readInt16BE(offset, noAssert) {
    if (!noAssert)
        checkOffset(offset, 2, this.length);
    var val = this[offset + 1] | (this[offset] << 8);
    return (val & 0x8000) ? val | 0xFFFF0000 : val;
};
Buffer.prototype.readInt32LE = function readInt32LE(offset, noAssert) {
    if (!noAssert)
        checkOffset(offset, 4, this.length);
    return (this[offset]) |
        (this[offset + 1] << 8) |
        (this[offset + 2] << 16) |
        (this[offset + 3] << 24);
};
Buffer.prototype.readInt32BE = function readInt32BE(offset, noAssert) {
    if (!noAssert)
        checkOffset(offset, 4, this.length);
    return (this[offset] << 24) |
        (this[offset + 1] << 16) |
        (this[offset + 2] << 8) |
        (this[offset + 3]);
};
Buffer.prototype.readFloatLE = function readFloatLE(offset, noAssert) {
    if (!noAssert)
        checkOffset(offset, 4, this.length);
    return ieee754.read(this, offset, true, 23, 4);
};
Buffer.prototype.readFloatBE = function readFloatBE(offset, noAssert) {
    if (!noAssert)
        checkOffset(offset, 4, this.length);
    return ieee754.read(this, offset, false, 23, 4);
};
Buffer.prototype.readDoubleLE = function readDoubleLE(offset, noAssert) {
    if (!noAssert)
        checkOffset(offset, 8, this.length);
    return ieee754.read(this, offset, true, 52, 8);
};
Buffer.prototype.readDoubleBE = function readDoubleBE(offset, noAssert) {
    if (!noAssert)
        checkOffset(offset, 8, this.length);
    return ieee754.read(this, offset, false, 52, 8);
};
function checkInt(buf, value, offset, ext, max, min) {
    if (!Buffer.isBuffer(buf))
        throw new TypeError('"buffer" argument must be a Buffer instance');
    if (value > max || value < min)
        throw new RangeError('"value" argument is out of bounds');
    if (offset + ext > buf.length)
        throw new RangeError('Index out of range');
}
Buffer.prototype.writeUIntLE = function writeUIntLE(value, offset, byteLength, noAssert) {
    value = +value;
    offset = offset | 0;
    byteLength = byteLength | 0;
    if (!noAssert) {
        var maxBytes = Math.pow(2, 8 * byteLength) - 1;
        checkInt(this, value, offset, byteLength, maxBytes, 0);
    }
    var mul = 1;
    var i = 0;
    this[offset] = value & 0xFF;
    while (++i < byteLength && (mul *= 0x100)) {
        this[offset + i] = (value / mul) & 0xFF;
    }
    return offset + byteLength;
};
Buffer.prototype.writeUIntBE = function writeUIntBE(value, offset, byteLength, noAssert) {
    value = +value;
    offset = offset | 0;
    byteLength = byteLength | 0;
    if (!noAssert) {
        var maxBytes = Math.pow(2, 8 * byteLength) - 1;
        checkInt(this, value, offset, byteLength, maxBytes, 0);
    }
    var i = byteLength - 1;
    var mul = 1;
    this[offset + i] = value & 0xFF;
    while (--i >= 0 && (mul *= 0x100)) {
        this[offset + i] = (value / mul) & 0xFF;
    }
    return offset + byteLength;
};
Buffer.prototype.writeUInt8 = function writeUInt8(value, offset, noAssert) {
    value = +value;
    offset = offset | 0;
    if (!noAssert)
        checkInt(this, value, offset, 1, 0xff, 0);
    if (!Buffer.TYPED_ARRAY_SUPPORT)
        value = Math.floor(value);
    this[offset] = (value & 0xff);
    return offset + 1;
};
function objectWriteUInt16(buf, value, offset, littleEndian) {
    if (value < 0)
        value = 0xffff + value + 1;
    for (var i = 0, j = Math.min(buf.length - offset, 2); i < j; ++i) {
        buf[offset + i] = (value & (0xff << (8 * (littleEndian ? i : 1 - i)))) >>>
            (littleEndian ? i : 1 - i) * 8;
    }
}
Buffer.prototype.writeUInt16LE = function writeUInt16LE(value, offset, noAssert) {
    value = +value;
    offset = offset | 0;
    if (!noAssert)
        checkInt(this, value, offset, 2, 0xffff, 0);
    if (Buffer.TYPED_ARRAY_SUPPORT) {
        this[offset] = (value & 0xff);
        this[offset + 1] = (value >>> 8);
    }
    else {
        objectWriteUInt16(this, value, offset, true);
    }
    return offset + 2;
};
Buffer.prototype.writeUInt16BE = function writeUInt16BE(value, offset, noAssert) {
    value = +value;
    offset = offset | 0;
    if (!noAssert)
        checkInt(this, value, offset, 2, 0xffff, 0);
    if (Buffer.TYPED_ARRAY_SUPPORT) {
        this[offset] = (value >>> 8);
        this[offset + 1] = (value & 0xff);
    }
    else {
        objectWriteUInt16(this, value, offset, false);
    }
    return offset + 2;
};
function objectWriteUInt32(buf, value, offset, littleEndian) {
    if (value < 0)
        value = 0xffffffff + value + 1;
    for (var i = 0, j = Math.min(buf.length - offset, 4); i < j; ++i) {
        buf[offset + i] = (value >>> (littleEndian ? i : 3 - i) * 8) & 0xff;
    }
}
Buffer.prototype.writeUInt32LE = function writeUInt32LE(value, offset, noAssert) {
    value = +value;
    offset = offset | 0;
    if (!noAssert)
        checkInt(this, value, offset, 4, 0xffffffff, 0);
    if (Buffer.TYPED_ARRAY_SUPPORT) {
        this[offset + 3] = (value >>> 24);
        this[offset + 2] = (value >>> 16);
        this[offset + 1] = (value >>> 8);
        this[offset] = (value & 0xff);
    }
    else {
        objectWriteUInt32(this, value, offset, true);
    }
    return offset + 4;
};
Buffer.prototype.writeUInt32BE = function writeUInt32BE(value, offset, noAssert) {
    value = +value;
    offset = offset | 0;
    if (!noAssert)
        checkInt(this, value, offset, 4, 0xffffffff, 0);
    if (Buffer.TYPED_ARRAY_SUPPORT) {
        this[offset] = (value >>> 24);
        this[offset + 1] = (value >>> 16);
        this[offset + 2] = (value >>> 8);
        this[offset + 3] = (value & 0xff);
    }
    else {
        objectWriteUInt32(this, value, offset, false);
    }
    return offset + 4;
};
Buffer.prototype.writeIntLE = function writeIntLE(value, offset, byteLength, noAssert) {
    value = +value;
    offset = offset | 0;
    if (!noAssert) {
        var limit = Math.pow(2, 8 * byteLength - 1);
        checkInt(this, value, offset, byteLength, limit - 1, -limit);
    }
    var i = 0;
    var mul = 1;
    var sub = 0;
    this[offset] = value & 0xFF;
    while (++i < byteLength && (mul *= 0x100)) {
        if (value < 0 && sub === 0 && this[offset + i - 1] !== 0) {
            sub = 1;
        }
        this[offset + i] = ((value / mul) >> 0) - sub & 0xFF;
    }
    return offset + byteLength;
};
Buffer.prototype.writeIntBE = function writeIntBE(value, offset, byteLength, noAssert) {
    value = +value;
    offset = offset | 0;
    if (!noAssert) {
        var limit = Math.pow(2, 8 * byteLength - 1);
        checkInt(this, value, offset, byteLength, limit - 1, -limit);
    }
    var i = byteLength - 1;
    var mul = 1;
    var sub = 0;
    this[offset + i] = value & 0xFF;
    while (--i >= 0 && (mul *= 0x100)) {
        if (value < 0 && sub === 0 && this[offset + i + 1] !== 0) {
            sub = 1;
        }
        this[offset + i] = ((value / mul) >> 0) - sub & 0xFF;
    }
    return offset + byteLength;
};
Buffer.prototype.writeInt8 = function writeInt8(value, offset, noAssert) {
    value = +value;
    offset = offset | 0;
    if (!noAssert)
        checkInt(this, value, offset, 1, 0x7f, -0x80);
    if (!Buffer.TYPED_ARRAY_SUPPORT)
        value = Math.floor(value);
    if (value < 0)
        value = 0xff + value + 1;
    this[offset] = (value & 0xff);
    return offset + 1;
};
Buffer.prototype.writeInt16LE = function writeInt16LE(value, offset, noAssert) {
    value = +value;
    offset = offset | 0;
    if (!noAssert)
        checkInt(this, value, offset, 2, 0x7fff, -0x8000);
    if (Buffer.TYPED_ARRAY_SUPPORT) {
        this[offset] = (value & 0xff);
        this[offset + 1] = (value >>> 8);
    }
    else {
        objectWriteUInt16(this, value, offset, true);
    }
    return offset + 2;
};
Buffer.prototype.writeInt16BE = function writeInt16BE(value, offset, noAssert) {
    value = +value;
    offset = offset | 0;
    if (!noAssert)
        checkInt(this, value, offset, 2, 0x7fff, -0x8000);
    if (Buffer.TYPED_ARRAY_SUPPORT) {
        this[offset] = (value >>> 8);
        this[offset + 1] = (value & 0xff);
    }
    else {
        objectWriteUInt16(this, value, offset, false);
    }
    return offset + 2;
};
Buffer.prototype.writeInt32LE = function writeInt32LE(value, offset, noAssert) {
    value = +value;
    offset = offset | 0;
    if (!noAssert)
        checkInt(this, value, offset, 4, 0x7fffffff, -0x80000000);
    if (Buffer.TYPED_ARRAY_SUPPORT) {
        this[offset] = (value & 0xff);
        this[offset + 1] = (value >>> 8);
        this[offset + 2] = (value >>> 16);
        this[offset + 3] = (value >>> 24);
    }
    else {
        objectWriteUInt32(this, value, offset, true);
    }
    return offset + 4;
};
Buffer.prototype.writeInt32BE = function writeInt32BE(value, offset, noAssert) {
    value = +value;
    offset = offset | 0;
    if (!noAssert)
        checkInt(this, value, offset, 4, 0x7fffffff, -0x80000000);
    if (value < 0)
        value = 0xffffffff + value + 1;
    if (Buffer.TYPED_ARRAY_SUPPORT) {
        this[offset] = (value >>> 24);
        this[offset + 1] = (value >>> 16);
        this[offset + 2] = (value >>> 8);
        this[offset + 3] = (value & 0xff);
    }
    else {
        objectWriteUInt32(this, value, offset, false);
    }
    return offset + 4;
};
function checkIEEE754(buf, value, offset, ext, max, min) {
    if (offset + ext > buf.length)
        throw new RangeError('Index out of range');
    if (offset < 0)
        throw new RangeError('Index out of range');
}
function writeFloat(buf, value, offset, littleEndian, noAssert) {
    if (!noAssert) {
        checkIEEE754(buf, value, offset, 4, 3.4028234663852886e+38, -3.4028234663852886e+38);
    }
    ieee754.write(buf, value, offset, littleEndian, 23, 4);
    return offset + 4;
}
Buffer.prototype.writeFloatLE = function writeFloatLE(value, offset, noAssert) {
    return writeFloat(this, value, offset, true, noAssert);
};
Buffer.prototype.writeFloatBE = function writeFloatBE(value, offset, noAssert) {
    return writeFloat(this, value, offset, false, noAssert);
};
function writeDouble(buf, value, offset, littleEndian, noAssert) {
    if (!noAssert) {
        checkIEEE754(buf, value, offset, 8, 1.7976931348623157E+308, -1.7976931348623157E+308);
    }
    ieee754.write(buf, value, offset, littleEndian, 52, 8);
    return offset + 8;
}
Buffer.prototype.writeDoubleLE = function writeDoubleLE(value, offset, noAssert) {
    return writeDouble(this, value, offset, true, noAssert);
};
Buffer.prototype.writeDoubleBE = function writeDoubleBE(value, offset, noAssert) {
    return writeDouble(this, value, offset, false, noAssert);
};
// copy(targetBuffer, targetStart=0, sourceStart=0, sourceEnd=buffer.length)
Buffer.prototype.copy = function copy(target, targetStart, start, end) {
    if (!start)
        start = 0;
    if (!end && end !== 0)
        end = this.length;
    if (targetStart >= target.length)
        targetStart = target.length;
    if (!targetStart)
        targetStart = 0;
    if (end > 0 && end < start)
        end = start;
    // Copy 0 bytes; we're done
    if (end === start)
        return 0;
    if (target.length === 0 || this.length === 0)
        return 0;
    // Fatal error conditions
    if (targetStart < 0) {
        throw new RangeError('targetStart out of bounds');
    }
    if (start < 0 || start >= this.length)
        throw new RangeError('sourceStart out of bounds');
    if (end < 0)
        throw new RangeError('sourceEnd out of bounds');
    // Are we oob?
    if (end > this.length)
        end = this.length;
    if (target.length - targetStart < end - start) {
        end = target.length - targetStart + start;
    }
    var len = end - start;
    var i;
    if (this === target && start < targetStart && targetStart < end) {
        // descending copy from end
        for (i = len - 1; i >= 0; --i) {
            target[i + targetStart] = this[i + start];
        }
    }
    else if (len < 1000 || !Buffer.TYPED_ARRAY_SUPPORT) {
        // ascending copy from start
        for (i = 0; i < len; ++i) {
            target[i + targetStart] = this[i + start];
        }
    }
    else {
        Uint8Array.prototype.set.call(target, this.subarray(start, start + len), targetStart);
    }
    return len;
};
// Usage:
//    buffer.fill(number[, offset[, end]])
//    buffer.fill(buffer[, offset[, end]])
//    buffer.fill(string[, offset[, end]][, encoding])
Buffer.prototype.fill = function fill(val, start, end, encoding) {
    // Handle string cases:
    if (typeof val === 'string') {
        if (typeof start === 'string') {
            encoding = start;
            start = 0;
            end = this.length;
        }
        else if (typeof end === 'string') {
            encoding = end;
            end = this.length;
        }
        if (val.length === 1) {
            var code = val.charCodeAt(0);
            if (code < 256) {
                val = code;
            }
        }
        if (encoding !== undefined && typeof encoding !== 'string') {
            throw new TypeError('encoding must be a string');
        }
        if (typeof encoding === 'string' && !Buffer.isEncoding(encoding)) {
            throw new TypeError('Unknown encoding: ' + encoding);
        }
    }
    else if (typeof val === 'number') {
        val = val & 255;
    }
    // Invalid ranges are not set to a default, so can range check early.
    if (start < 0 || this.length < start || this.length < end) {
        throw new RangeError('Out of range index');
    }
    if (end <= start) {
        return this;
    }
    start = start >>> 0;
    end = end === undefined ? this.length : end >>> 0;
    if (!val)
        val = 0;
    var i;
    if (typeof val === 'number') {
        for (i = start; i < end; ++i) {
            this[i] = val;
        }
    }
    else {
        var bytes = Buffer.isBuffer(val)
            ? val
            : utf8ToBytes(new Buffer(val, encoding).toString());
        var len = bytes.length;
        for (i = 0; i < end - start; ++i) {
            this[i + start] = bytes[i % len];
        }
    }
    return this;
};
// HELPER FUNCTIONS
// ================
var INVALID_BASE64_RE = /[^+\/0-9A-Za-z-_]/g;
function base64clean(str) {
    // Node strips out invalid characters like \n and \t from the string, base64-js does not
    str = stringtrim(str).replace(INVALID_BASE64_RE, '');
    // Node converts strings with length < 2 to ''
    if (str.length < 2)
        return '';
    // Node allows for non-padded base64 strings (missing trailing ===), base64-js does not
    while (str.length % 4 !== 0) {
        str = str + '=';
    }
    return str;
}
function stringtrim(str) {
    if (str.trim)
        return str.trim();
    return str.replace(/^\s+|\s+$/g, '');
}
function toHex(n) {
    if (n < 16)
        return '0' + n.toString(16);
    return n.toString(16);
}
function utf8ToBytes(string, units) {
    units = units || Infinity;
    var codePoint;
    var length = string.length;
    var leadSurrogate = null;
    var bytes = [];
    for (var i = 0; i < length; ++i) {
        codePoint = string.charCodeAt(i);
        // is surrogate component
        if (codePoint > 0xD7FF && codePoint < 0xE000) {
            // last char was a lead
            if (!leadSurrogate) {
                // no lead yet
                if (codePoint > 0xDBFF) {
                    // unexpected trail
                    if ((units -= 3) > -1)
                        bytes.push(0xEF, 0xBF, 0xBD);
                    continue;
                }
                else if (i + 1 === length) {
                    // unpaired lead
                    if ((units -= 3) > -1)
                        bytes.push(0xEF, 0xBF, 0xBD);
                    continue;
                }
                // valid lead
                leadSurrogate = codePoint;
                continue;
            }
            // 2 leads in a row
            if (codePoint < 0xDC00) {
                if ((units -= 3) > -1)
                    bytes.push(0xEF, 0xBF, 0xBD);
                leadSurrogate = codePoint;
                continue;
            }
            // valid surrogate pair
            codePoint = (leadSurrogate - 0xD800 << 10 | codePoint - 0xDC00) + 0x10000;
        }
        else if (leadSurrogate) {
            // valid bmp char, but last char was a lead
            if ((units -= 3) > -1)
                bytes.push(0xEF, 0xBF, 0xBD);
        }
        leadSurrogate = null;
        // encode utf8
        if (codePoint < 0x80) {
            if ((units -= 1) < 0)
                break;
            bytes.push(codePoint);
        }
        else if (codePoint < 0x800) {
            if ((units -= 2) < 0)
                break;
            bytes.push(codePoint >> 0x6 | 0xC0, codePoint & 0x3F | 0x80);
        }
        else if (codePoint < 0x10000) {
            if ((units -= 3) < 0)
                break;
            bytes.push(codePoint >> 0xC | 0xE0, codePoint >> 0x6 & 0x3F | 0x80, codePoint & 0x3F | 0x80);
        }
        else if (codePoint < 0x110000) {
            if ((units -= 4) < 0)
                break;
            bytes.push(codePoint >> 0x12 | 0xF0, codePoint >> 0xC & 0x3F | 0x80, codePoint >> 0x6 & 0x3F | 0x80, codePoint & 0x3F | 0x80);
        }
        else {
            throw new Error('Invalid code point');
        }
    }
    return bytes;
}
function asciiToBytes(str) {
    var byteArray = [];
    for (var i = 0; i < str.length; ++i) {
        // Node's code seems to be doing this and not & 0x7F..
        byteArray.push(str.charCodeAt(i) & 0xFF);
    }
    return byteArray;
}
function utf16leToBytes(str, units) {
    var c, hi, lo;
    var byteArray = [];
    for (var i = 0; i < str.length; ++i) {
        if ((units -= 2) < 0)
            break;
        c = str.charCodeAt(i);
        hi = c >> 8;
        lo = c % 256;
        byteArray.push(lo);
        byteArray.push(hi);
    }
    return byteArray;
}
function base64ToBytes(str) {
    return base64.toByteArray(base64clean(str));
}
function blitBuffer(src, dst, offset, length) {
    for (var i = 0; i < length; ++i) {
        if ((i + offset >= dst.length) || (i >= src.length))
            break;
        dst[i + offset] = src[i];
    }
    return i;
}
function isnan(val) {
    return val !== val; // eslint-disable-line no-self-compare
}

/* WEBPACK VAR INJECTION */}.call(this, __webpack_require__(/*! ./../webpack/buildin/global.js */ "./node_modules/webpack/buildin/global.js")))

/***/ }),

/***/ "./node_modules/buffer/node_modules/isarray/index.js":
/*!***********************************************************!*\
  !*** ./node_modules/buffer/node_modules/isarray/index.js ***!
  \***********************************************************/
/*! no static exports found */
/***/ (function(module, exports) {

var toString = {}.toString;
module.exports = Array.isArray || function (arr) {
    return toString.call(arr) == '[object Array]';
};


/***/ }),

/***/ "./node_modules/es6-promise/dist/es6-promise.js":
/*!******************************************************!*\
  !*** ./node_modules/es6-promise/dist/es6-promise.js ***!
  \******************************************************/
/*! no static exports found */
/***/ (function(module, exports, __webpack_require__) {

/* WEBPACK VAR INJECTION */(function(process, global) {/*!
 * @overview es6-promise - a tiny implementation of Promises/A+.
 * @copyright Copyright (c) 2014 Yehuda Katz, Tom Dale, Stefan Penner and contributors (Conversion to ES6 API by Jake Archibald)
 * @license   Licensed under MIT license
 *            See https://raw.githubusercontent.com/stefanpenner/es6-promise/master/LICENSE
 * @version   v4.2.8+1e68dce6
 */
(function (global, factory) {
     true ? module.exports = factory() :
        undefined;
}(this, (function () {
    'use strict';
    function objectOrFunction(x) {
        var type = typeof x;
        return x !== null && (type === 'object' || type === 'function');
    }
    function isFunction(x) {
        return typeof x === 'function';
    }
    var _isArray = void 0;
    if (Array.isArray) {
        _isArray = Array.isArray;
    }
    else {
        _isArray = function (x) {
            return Object.prototype.toString.call(x) === '[object Array]';
        };
    }
    var isArray = _isArray;
    var len = 0;
    var vertxNext = void 0;
    var customSchedulerFn = void 0;
    var asap = function asap(callback, arg) {
        queue[len] = callback;
        queue[len + 1] = arg;
        len += 2;
        if (len === 2) {
            // If len is 2, that means that we need to schedule an async flush.
            // If additional callbacks are queued before the queue is flushed, they
            // will be processed by this flush that we are scheduling.
            if (customSchedulerFn) {
                customSchedulerFn(flush);
            }
            else {
                scheduleFlush();
            }
        }
    };
    function setScheduler(scheduleFn) {
        customSchedulerFn = scheduleFn;
    }
    function setAsap(asapFn) {
        asap = asapFn;
    }
    var browserWindow = typeof window !== 'undefined' ? window : undefined;
    var browserGlobal = browserWindow || {};
    var BrowserMutationObserver = browserGlobal.MutationObserver || browserGlobal.WebKitMutationObserver;
    var isNode = typeof self === 'undefined' && typeof process !== 'undefined' && {}.toString.call(process) === '[object process]';
    // test for web worker but not in IE10
    var isWorker = typeof Uint8ClampedArray !== 'undefined' && typeof importScripts !== 'undefined' && typeof MessageChannel !== 'undefined';
    // node
    function useNextTick() {
        // node version 0.10.x displays a deprecation warning when nextTick is used recursively
        // see https://github.com/cujojs/when/issues/410 for details
        return function () {
            return process.nextTick(flush);
        };
    }
    // vertx
    function useVertxTimer() {
        if (typeof vertxNext !== 'undefined') {
            return function () {
                vertxNext(flush);
            };
        }
        return useSetTimeout();
    }
    function useMutationObserver() {
        var iterations = 0;
        var observer = new BrowserMutationObserver(flush);
        var node = document.createTextNode('');
        observer.observe(node, { characterData: true });
        return function () {
            node.data = iterations = ++iterations % 2;
        };
    }
    // web worker
    function useMessageChannel() {
        var channel = new MessageChannel();
        channel.port1.onmessage = flush;
        return function () {
            return channel.port2.postMessage(0);
        };
    }
    function useSetTimeout() {
        // Store setTimeout reference so es6-promise will be unaffected by
        // other code modifying setTimeout (like sinon.useFakeTimers())
        var globalSetTimeout = setTimeout;
        return function () {
            return globalSetTimeout(flush, 1);
        };
    }
    var queue = new Array(1000);
    function flush() {
        for (var i = 0; i < len; i += 2) {
            var callback = queue[i];
            var arg = queue[i + 1];
            callback(arg);
            queue[i] = undefined;
            queue[i + 1] = undefined;
        }
        len = 0;
    }
    function attemptVertx() {
        try {
            var vertx = Function('return this')().require('vertx');
            vertxNext = vertx.runOnLoop || vertx.runOnContext;
            return useVertxTimer();
        }
        catch (e) {
            return useSetTimeout();
        }
    }
    var scheduleFlush = void 0;
    // Decide what async method to use to triggering processing of queued callbacks:
    if (isNode) {
        scheduleFlush = useNextTick();
    }
    else if (BrowserMutationObserver) {
        scheduleFlush = useMutationObserver();
    }
    else if (isWorker) {
        scheduleFlush = useMessageChannel();
    }
    else if (browserWindow === undefined && "function" === 'function') {
        scheduleFlush = attemptVertx();
    }
    else {
        scheduleFlush = useSetTimeout();
    }
    function then(onFulfillment, onRejection) {
        var parent = this;
        var child = new this.constructor(noop);
        if (child[PROMISE_ID] === undefined) {
            makePromise(child);
        }
        var _state = parent._state;
        if (_state) {
            var callback = arguments[_state - 1];
            asap(function () {
                return invokeCallback(_state, child, callback, parent._result);
            });
        }
        else {
            subscribe(parent, child, onFulfillment, onRejection);
        }
        return child;
    }
    /**
      `Promise.resolve` returns a promise that will become resolved with the
      passed `value`. It is shorthand for the following:
    
      ```javascript
      let promise = new Promise(function(resolve, reject){
        resolve(1);
      });
    
      promise.then(function(value){
        // value === 1
      });
      ```
    
      Instead of writing the above, your code now simply becomes the following:
    
      ```javascript
      let promise = Promise.resolve(1);
    
      promise.then(function(value){
        // value === 1
      });
      ```
    
      @method resolve
      @static
      @param {Any} value value that the returned promise will be resolved with
      Useful for tooling.
      @return {Promise} a promise that will become fulfilled with the given
      `value`
    */
    function resolve$1(object) {
        /*jshint validthis:true */
        var Constructor = this;
        if (object && typeof object === 'object' && object.constructor === Constructor) {
            return object;
        }
        var promise = new Constructor(noop);
        resolve(promise, object);
        return promise;
    }
    var PROMISE_ID = Math.random().toString(36).substring(2);
    function noop() { }
    var PENDING = void 0;
    var FULFILLED = 1;
    var REJECTED = 2;
    function selfFulfillment() {
        return new TypeError("You cannot resolve a promise with itself");
    }
    function cannotReturnOwn() {
        return new TypeError('A promises callback cannot return that same promise.');
    }
    function tryThen(then$$1, value, fulfillmentHandler, rejectionHandler) {
        try {
            then$$1.call(value, fulfillmentHandler, rejectionHandler);
        }
        catch (e) {
            return e;
        }
    }
    function handleForeignThenable(promise, thenable, then$$1) {
        asap(function (promise) {
            var sealed = false;
            var error = tryThen(then$$1, thenable, function (value) {
                if (sealed) {
                    return;
                }
                sealed = true;
                if (thenable !== value) {
                    resolve(promise, value);
                }
                else {
                    fulfill(promise, value);
                }
            }, function (reason) {
                if (sealed) {
                    return;
                }
                sealed = true;
                reject(promise, reason);
            }, 'Settle: ' + (promise._label || ' unknown promise'));
            if (!sealed && error) {
                sealed = true;
                reject(promise, error);
            }
        }, promise);
    }
    function handleOwnThenable(promise, thenable) {
        if (thenable._state === FULFILLED) {
            fulfill(promise, thenable._result);
        }
        else if (thenable._state === REJECTED) {
            reject(promise, thenable._result);
        }
        else {
            subscribe(thenable, undefined, function (value) {
                return resolve(promise, value);
            }, function (reason) {
                return reject(promise, reason);
            });
        }
    }
    function handleMaybeThenable(promise, maybeThenable, then$$1) {
        if (maybeThenable.constructor === promise.constructor && then$$1 === then && maybeThenable.constructor.resolve === resolve$1) {
            handleOwnThenable(promise, maybeThenable);
        }
        else {
            if (then$$1 === undefined) {
                fulfill(promise, maybeThenable);
            }
            else if (isFunction(then$$1)) {
                handleForeignThenable(promise, maybeThenable, then$$1);
            }
            else {
                fulfill(promise, maybeThenable);
            }
        }
    }
    function resolve(promise, value) {
        if (promise === value) {
            reject(promise, selfFulfillment());
        }
        else if (objectOrFunction(value)) {
            var then$$1 = void 0;
            try {
                then$$1 = value.then;
            }
            catch (error) {
                reject(promise, error);
                return;
            }
            handleMaybeThenable(promise, value, then$$1);
        }
        else {
            fulfill(promise, value);
        }
    }
    function publishRejection(promise) {
        if (promise._onerror) {
            promise._onerror(promise._result);
        }
        publish(promise);
    }
    function fulfill(promise, value) {
        if (promise._state !== PENDING) {
            return;
        }
        promise._result = value;
        promise._state = FULFILLED;
        if (promise._subscribers.length !== 0) {
            asap(publish, promise);
        }
    }
    function reject(promise, reason) {
        if (promise._state !== PENDING) {
            return;
        }
        promise._state = REJECTED;
        promise._result = reason;
        asap(publishRejection, promise);
    }
    function subscribe(parent, child, onFulfillment, onRejection) {
        var _subscribers = parent._subscribers;
        var length = _subscribers.length;
        parent._onerror = null;
        _subscribers[length] = child;
        _subscribers[length + FULFILLED] = onFulfillment;
        _subscribers[length + REJECTED] = onRejection;
        if (length === 0 && parent._state) {
            asap(publish, parent);
        }
    }
    function publish(promise) {
        var subscribers = promise._subscribers;
        var settled = promise._state;
        if (subscribers.length === 0) {
            return;
        }
        var child = void 0, callback = void 0, detail = promise._result;
        for (var i = 0; i < subscribers.length; i += 3) {
            child = subscribers[i];
            callback = subscribers[i + settled];
            if (child) {
                invokeCallback(settled, child, callback, detail);
            }
            else {
                callback(detail);
            }
        }
        promise._subscribers.length = 0;
    }
    function invokeCallback(settled, promise, callback, detail) {
        var hasCallback = isFunction(callback), value = void 0, error = void 0, succeeded = true;
        if (hasCallback) {
            try {
                value = callback(detail);
            }
            catch (e) {
                succeeded = false;
                error = e;
            }
            if (promise === value) {
                reject(promise, cannotReturnOwn());
                return;
            }
        }
        else {
            value = detail;
        }
        if (promise._state !== PENDING) {
            // noop
        }
        else if (hasCallback && succeeded) {
            resolve(promise, value);
        }
        else if (succeeded === false) {
            reject(promise, error);
        }
        else if (settled === FULFILLED) {
            fulfill(promise, value);
        }
        else if (settled === REJECTED) {
            reject(promise, value);
        }
    }
    function initializePromise(promise, resolver) {
        try {
            resolver(function resolvePromise(value) {
                resolve(promise, value);
            }, function rejectPromise(reason) {
                reject(promise, reason);
            });
        }
        catch (e) {
            reject(promise, e);
        }
    }
    var id = 0;
    function nextId() {
        return id++;
    }
    function makePromise(promise) {
        promise[PROMISE_ID] = id++;
        promise._state = undefined;
        promise._result = undefined;
        promise._subscribers = [];
    }
    function validationError() {
        return new Error('Array Methods must be provided an Array');
    }
    var Enumerator = function () {
        function Enumerator(Constructor, input) {
            this._instanceConstructor = Constructor;
            this.promise = new Constructor(noop);
            if (!this.promise[PROMISE_ID]) {
                makePromise(this.promise);
            }
            if (isArray(input)) {
                this.length = input.length;
                this._remaining = input.length;
                this._result = new Array(this.length);
                if (this.length === 0) {
                    fulfill(this.promise, this._result);
                }
                else {
                    this.length = this.length || 0;
                    this._enumerate(input);
                    if (this._remaining === 0) {
                        fulfill(this.promise, this._result);
                    }
                }
            }
            else {
                reject(this.promise, validationError());
            }
        }
        Enumerator.prototype._enumerate = function _enumerate(input) {
            for (var i = 0; this._state === PENDING && i < input.length; i++) {
                this._eachEntry(input[i], i);
            }
        };
        Enumerator.prototype._eachEntry = function _eachEntry(entry, i) {
            var c = this._instanceConstructor;
            var resolve$$1 = c.resolve;
            if (resolve$$1 === resolve$1) {
                var _then = void 0;
                var error = void 0;
                var didError = false;
                try {
                    _then = entry.then;
                }
                catch (e) {
                    didError = true;
                    error = e;
                }
                if (_then === then && entry._state !== PENDING) {
                    this._settledAt(entry._state, i, entry._result);
                }
                else if (typeof _then !== 'function') {
                    this._remaining--;
                    this._result[i] = entry;
                }
                else if (c === Promise$1) {
                    var promise = new c(noop);
                    if (didError) {
                        reject(promise, error);
                    }
                    else {
                        handleMaybeThenable(promise, entry, _then);
                    }
                    this._willSettleAt(promise, i);
                }
                else {
                    this._willSettleAt(new c(function (resolve$$1) {
                        return resolve$$1(entry);
                    }), i);
                }
            }
            else {
                this._willSettleAt(resolve$$1(entry), i);
            }
        };
        Enumerator.prototype._settledAt = function _settledAt(state, i, value) {
            var promise = this.promise;
            if (promise._state === PENDING) {
                this._remaining--;
                if (state === REJECTED) {
                    reject(promise, value);
                }
                else {
                    this._result[i] = value;
                }
            }
            if (this._remaining === 0) {
                fulfill(promise, this._result);
            }
        };
        Enumerator.prototype._willSettleAt = function _willSettleAt(promise, i) {
            var enumerator = this;
            subscribe(promise, undefined, function (value) {
                return enumerator._settledAt(FULFILLED, i, value);
            }, function (reason) {
                return enumerator._settledAt(REJECTED, i, reason);
            });
        };
        return Enumerator;
    }();
    /**
      `Promise.all` accepts an array of promises, and returns a new promise which
      is fulfilled with an array of fulfillment values for the passed promises, or
      rejected with the reason of the first passed promise to be rejected. It casts all
      elements of the passed iterable to promises as it runs this algorithm.
    
      Example:
    
      ```javascript
      let promise1 = resolve(1);
      let promise2 = resolve(2);
      let promise3 = resolve(3);
      let promises = [ promise1, promise2, promise3 ];
    
      Promise.all(promises).then(function(array){
        // The array here would be [ 1, 2, 3 ];
      });
      ```
    
      If any of the `promises` given to `all` are rejected, the first promise
      that is rejected will be given as an argument to the returned promises's
      rejection handler. For example:
    
      Example:
    
      ```javascript
      let promise1 = resolve(1);
      let promise2 = reject(new Error("2"));
      let promise3 = reject(new Error("3"));
      let promises = [ promise1, promise2, promise3 ];
    
      Promise.all(promises).then(function(array){
        // Code here never runs because there are rejected promises!
      }, function(error) {
        // error.message === "2"
      });
      ```
    
      @method all
      @static
      @param {Array} entries array of promises
      @param {String} label optional string for labeling the promise.
      Useful for tooling.
      @return {Promise} promise that is fulfilled when all `promises` have been
      fulfilled, or rejected if any of them become rejected.
      @static
    */
    function all(entries) {
        return new Enumerator(this, entries).promise;
    }
    /**
      `Promise.race` returns a new promise which is settled in the same way as the
      first passed promise to settle.
    
      Example:
    
      ```javascript
      let promise1 = new Promise(function(resolve, reject){
        setTimeout(function(){
          resolve('promise 1');
        }, 200);
      });
    
      let promise2 = new Promise(function(resolve, reject){
        setTimeout(function(){
          resolve('promise 2');
        }, 100);
      });
    
      Promise.race([promise1, promise2]).then(function(result){
        // result === 'promise 2' because it was resolved before promise1
        // was resolved.
      });
      ```
    
      `Promise.race` is deterministic in that only the state of the first
      settled promise matters. For example, even if other promises given to the
      `promises` array argument are resolved, but the first settled promise has
      become rejected before the other promises became fulfilled, the returned
      promise will become rejected:
    
      ```javascript
      let promise1 = new Promise(function(resolve, reject){
        setTimeout(function(){
          resolve('promise 1');
        }, 200);
      });
    
      let promise2 = new Promise(function(resolve, reject){
        setTimeout(function(){
          reject(new Error('promise 2'));
        }, 100);
      });
    
      Promise.race([promise1, promise2]).then(function(result){
        // Code here never runs
      }, function(reason){
        // reason.message === 'promise 2' because promise 2 became rejected before
        // promise 1 became fulfilled
      });
      ```
    
      An example real-world use case is implementing timeouts:
    
      ```javascript
      Promise.race([ajax('foo.json'), timeout(5000)])
      ```
    
      @method race
      @static
      @param {Array} promises array of promises to observe
      Useful for tooling.
      @return {Promise} a promise which settles in the same way as the first passed
      promise to settle.
    */
    function race(entries) {
        /*jshint validthis:true */
        var Constructor = this;
        if (!isArray(entries)) {
            return new Constructor(function (_, reject) {
                return reject(new TypeError('You must pass an array to race.'));
            });
        }
        else {
            return new Constructor(function (resolve, reject) {
                var length = entries.length;
                for (var i = 0; i < length; i++) {
                    Constructor.resolve(entries[i]).then(resolve, reject);
                }
            });
        }
    }
    /**
      `Promise.reject` returns a promise rejected with the passed `reason`.
      It is shorthand for the following:
    
      ```javascript
      let promise = new Promise(function(resolve, reject){
        reject(new Error('WHOOPS'));
      });
    
      promise.then(function(value){
        // Code here doesn't run because the promise is rejected!
      }, function(reason){
        // reason.message === 'WHOOPS'
      });
      ```
    
      Instead of writing the above, your code now simply becomes the following:
    
      ```javascript
      let promise = Promise.reject(new Error('WHOOPS'));
    
      promise.then(function(value){
        // Code here doesn't run because the promise is rejected!
      }, function(reason){
        // reason.message === 'WHOOPS'
      });
      ```
    
      @method reject
      @static
      @param {Any} reason value that the returned promise will be rejected with.
      Useful for tooling.
      @return {Promise} a promise rejected with the given `reason`.
    */
    function reject$1(reason) {
        /*jshint validthis:true */
        var Constructor = this;
        var promise = new Constructor(noop);
        reject(promise, reason);
        return promise;
    }
    function needsResolver() {
        throw new TypeError('You must pass a resolver function as the first argument to the promise constructor');
    }
    function needsNew() {
        throw new TypeError("Failed to construct 'Promise': Please use the 'new' operator, this object constructor cannot be called as a function.");
    }
    /**
      Promise objects represent the eventual result of an asynchronous operation. The
      primary way of interacting with a promise is through its `then` method, which
      registers callbacks to receive either a promise's eventual value or the reason
      why the promise cannot be fulfilled.
    
      Terminology
      -----------
    
      - `promise` is an object or function with a `then` method whose behavior conforms to this specification.
      - `thenable` is an object or function that defines a `then` method.
      - `value` is any legal JavaScript value (including undefined, a thenable, or a promise).
      - `exception` is a value that is thrown using the throw statement.
      - `reason` is a value that indicates why a promise was rejected.
      - `settled` the final resting state of a promise, fulfilled or rejected.
    
      A promise can be in one of three states: pending, fulfilled, or rejected.
    
      Promises that are fulfilled have a fulfillment value and are in the fulfilled
      state.  Promises that are rejected have a rejection reason and are in the
      rejected state.  A fulfillment value is never a thenable.
    
      Promises can also be said to *resolve* a value.  If this value is also a
      promise, then the original promise's settled state will match the value's
      settled state.  So a promise that *resolves* a promise that rejects will
      itself reject, and a promise that *resolves* a promise that fulfills will
      itself fulfill.
    
    
      Basic Usage:
      ------------
    
      ```js
      let promise = new Promise(function(resolve, reject) {
        // on success
        resolve(value);
    
        // on failure
        reject(reason);
      });
    
      promise.then(function(value) {
        // on fulfillment
      }, function(reason) {
        // on rejection
      });
      ```
    
      Advanced Usage:
      ---------------
    
      Promises shine when abstracting away asynchronous interactions such as
      `XMLHttpRequest`s.
    
      ```js
      function getJSON(url) {
        return new Promise(function(resolve, reject){
          let xhr = new XMLHttpRequest();
    
          xhr.open('GET', url);
          xhr.onreadystatechange = handler;
          xhr.responseType = 'json';
          xhr.setRequestHeader('Accept', 'application/json');
          xhr.send();
    
          function handler() {
            if (this.readyState === this.DONE) {
              if (this.status === 200) {
                resolve(this.response);
              } else {
                reject(new Error('getJSON: `' + url + '` failed with status: [' + this.status + ']'));
              }
            }
          };
        });
      }
    
      getJSON('/posts.json').then(function(json) {
        // on fulfillment
      }, function(reason) {
        // on rejection
      });
      ```
    
      Unlike callbacks, promises are great composable primitives.
    
      ```js
      Promise.all([
        getJSON('/posts'),
        getJSON('/comments')
      ]).then(function(values){
        values[0] // => postsJSON
        values[1] // => commentsJSON
    
        return values;
      });
      ```
    
      @class Promise
      @param {Function} resolver
      Useful for tooling.
      @constructor
    */
    var Promise$1 = function () {
        function Promise(resolver) {
            this[PROMISE_ID] = nextId();
            this._result = this._state = undefined;
            this._subscribers = [];
            if (noop !== resolver) {
                typeof resolver !== 'function' && needsResolver();
                this instanceof Promise ? initializePromise(this, resolver) : needsNew();
            }
        }
        /**
        The primary way of interacting with a promise is through its `then` method,
        which registers callbacks to receive either a promise's eventual value or the
        reason why the promise cannot be fulfilled.
         ```js
        findUser().then(function(user){
          // user is available
        }, function(reason){
          // user is unavailable, and you are given the reason why
        });
        ```
         Chaining
        --------
         The return value of `then` is itself a promise.  This second, 'downstream'
        promise is resolved with the return value of the first promise's fulfillment
        or rejection handler, or rejected if the handler throws an exception.
         ```js
        findUser().then(function (user) {
          return user.name;
        }, function (reason) {
          return 'default name';
        }).then(function (userName) {
          // If `findUser` fulfilled, `userName` will be the user's name, otherwise it
          // will be `'default name'`
        });
         findUser().then(function (user) {
          throw new Error('Found user, but still unhappy');
        }, function (reason) {
          throw new Error('`findUser` rejected and we're unhappy');
        }).then(function (value) {
          // never reached
        }, function (reason) {
          // if `findUser` fulfilled, `reason` will be 'Found user, but still unhappy'.
          // If `findUser` rejected, `reason` will be '`findUser` rejected and we're unhappy'.
        });
        ```
        If the downstream promise does not specify a rejection handler, rejection reasons will be propagated further downstream.
         ```js
        findUser().then(function (user) {
          throw new PedagogicalException('Upstream error');
        }).then(function (value) {
          // never reached
        }).then(function (value) {
          // never reached
        }, function (reason) {
          // The `PedgagocialException` is propagated all the way down to here
        });
        ```
         Assimilation
        ------------
         Sometimes the value you want to propagate to a downstream promise can only be
        retrieved asynchronously. This can be achieved by returning a promise in the
        fulfillment or rejection handler. The downstream promise will then be pending
        until the returned promise is settled. This is called *assimilation*.
         ```js
        findUser().then(function (user) {
          return findCommentsByAuthor(user);
        }).then(function (comments) {
          // The user's comments are now available
        });
        ```
         If the assimliated promise rejects, then the downstream promise will also reject.
         ```js
        findUser().then(function (user) {
          return findCommentsByAuthor(user);
        }).then(function (comments) {
          // If `findCommentsByAuthor` fulfills, we'll have the value here
        }, function (reason) {
          // If `findCommentsByAuthor` rejects, we'll have the reason here
        });
        ```
         Simple Example
        --------------
         Synchronous Example
         ```javascript
        let result;
         try {
          result = findResult();
          // success
        } catch(reason) {
          // failure
        }
        ```
         Errback Example
         ```js
        findResult(function(result, err){
          if (err) {
            // failure
          } else {
            // success
          }
        });
        ```
         Promise Example;
         ```javascript
        findResult().then(function(result){
          // success
        }, function(reason){
          // failure
        });
        ```
         Advanced Example
        --------------
         Synchronous Example
         ```javascript
        let author, books;
         try {
          author = findAuthor();
          books  = findBooksByAuthor(author);
          // success
        } catch(reason) {
          // failure
        }
        ```
         Errback Example
         ```js
         function foundBooks(books) {
         }
         function failure(reason) {
         }
         findAuthor(function(author, err){
          if (err) {
            failure(err);
            // failure
          } else {
            try {
              findBoooksByAuthor(author, function(books, err) {
                if (err) {
                  failure(err);
                } else {
                  try {
                    foundBooks(books);
                  } catch(reason) {
                    failure(reason);
                  }
                }
              });
            } catch(error) {
              failure(err);
            }
            // success
          }
        });
        ```
         Promise Example;
         ```javascript
        findAuthor().
          then(findBooksByAuthor).
          then(function(books){
            // found books
        }).catch(function(reason){
          // something went wrong
        });
        ```
         @method then
        @param {Function} onFulfilled
        @param {Function} onRejected
        Useful for tooling.
        @return {Promise}
        */
        /**
        `catch` is simply sugar for `then(undefined, onRejection)` which makes it the same
        as the catch block of a try/catch statement.
        ```js
        function findAuthor(){
        throw new Error('couldn't find that author');
        }
        // synchronous
        try {
        findAuthor();
        } catch(reason) {
        // something went wrong
        }
        // async with promises
        findAuthor().catch(function(reason){
        // something went wrong
        });
        ```
        @method catch
        @param {Function} onRejection
        Useful for tooling.
        @return {Promise}
        */
        Promise.prototype.catch = function _catch(onRejection) {
            return this.then(null, onRejection);
        };
        /**
          `finally` will be invoked regardless of the promise's fate just as native
          try/catch/finally behaves
        
          Synchronous example:
        
          ```js
          findAuthor() {
            if (Math.random() > 0.5) {
              throw new Error();
            }
            return new Author();
          }
        
          try {
            return findAuthor(); // succeed or fail
          } catch(error) {
            return findOtherAuther();
          } finally {
            // always runs
            // doesn't affect the return value
          }
          ```
        
          Asynchronous example:
        
          ```js
          findAuthor().catch(function(reason){
            return findOtherAuther();
          }).finally(function(){
            // author was either found, or not
          });
          ```
        
          @method finally
          @param {Function} callback
          @return {Promise}
        */
        Promise.prototype.finally = function _finally(callback) {
            var promise = this;
            var constructor = promise.constructor;
            if (isFunction(callback)) {
                return promise.then(function (value) {
                    return constructor.resolve(callback()).then(function () {
                        return value;
                    });
                }, function (reason) {
                    return constructor.resolve(callback()).then(function () {
                        throw reason;
                    });
                });
            }
            return promise.then(callback, callback);
        };
        return Promise;
    }();
    Promise$1.prototype.then = then;
    Promise$1.all = all;
    Promise$1.race = race;
    Promise$1.resolve = resolve$1;
    Promise$1.reject = reject$1;
    Promise$1._setScheduler = setScheduler;
    Promise$1._setAsap = setAsap;
    Promise$1._asap = asap;
    /*global self*/
    function polyfill() {
        var local = void 0;
        if (typeof global !== 'undefined') {
            local = global;
        }
        else if (typeof self !== 'undefined') {
            local = self;
        }
        else {
            try {
                local = Function('return this')();
            }
            catch (e) {
                throw new Error('polyfill failed because global object is unavailable in this environment');
            }
        }
        var P = local.Promise;
        if (P) {
            var promiseToString = null;
            try {
                promiseToString = Object.prototype.toString.call(P.resolve());
            }
            catch (e) {
                // silently ignored
            }
            if (promiseToString === '[object Promise]' && !P.cast) {
                return;
            }
        }
        local.Promise = Promise$1;
    }
    // Strange compat..
    Promise$1.polyfill = polyfill;
    Promise$1.Promise = Promise$1;
    return Promise$1;
})));

/* WEBPACK VAR INJECTION */}.call(this, __webpack_require__(/*! ./../../process/browser.js */ "./node_modules/process/browser.js"), __webpack_require__(/*! ./../../webpack/buildin/global.js */ "./node_modules/webpack/buildin/global.js")))

/***/ }),

/***/ "./node_modules/events/events.js":
/*!***************************************!*\
  !*** ./node_modules/events/events.js ***!
  \***************************************/
/*! no static exports found */
/***/ (function(module, exports, __webpack_require__) {

"use strict";
// Copyright Joyent, Inc. and other Node contributors.
//
// Permission is hereby granted, free of charge, to any person obtaining a
// copy of this software and associated documentation files (the
// "Software"), to deal in the Software without restriction, including
// without limitation the rights to use, copy, modify, merge, publish,
// distribute, sublicense, and/or sell copies of the Software, and to permit
// persons to whom the Software is furnished to do so, subject to the
// following conditions:
//
// The above copyright notice and this permission notice shall be included
// in all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS
// OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
// MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN
// NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM,
// DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
// OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE
// USE OR OTHER DEALINGS IN THE SOFTWARE.

var R = typeof Reflect === 'object' ? Reflect : null;
var ReflectApply = R && typeof R.apply === 'function'
    ? R.apply
    : function ReflectApply(target, receiver, args) {
        return Function.prototype.apply.call(target, receiver, args);
    };
var ReflectOwnKeys;
if (R && typeof R.ownKeys === 'function') {
    ReflectOwnKeys = R.ownKeys;
}
else if (Object.getOwnPropertySymbols) {
    ReflectOwnKeys = function ReflectOwnKeys(target) {
        return Object.getOwnPropertyNames(target)
            .concat(Object.getOwnPropertySymbols(target));
    };
}
else {
    ReflectOwnKeys = function ReflectOwnKeys(target) {
        return Object.getOwnPropertyNames(target);
    };
}
function ProcessEmitWarning(warning) {
    if (console && console.warn)
        console.warn(warning);
}
var NumberIsNaN = Number.isNaN || function NumberIsNaN(value) {
    return value !== value;
};
function EventEmitter() {
    EventEmitter.init.call(this);
}
module.exports = EventEmitter;
module.exports.once = once;
// Backwards-compat with node 0.10.x
EventEmitter.EventEmitter = EventEmitter;
EventEmitter.prototype._events = undefined;
EventEmitter.prototype._eventsCount = 0;
EventEmitter.prototype._maxListeners = undefined;
// By default EventEmitters will print a warning if more than 10 listeners are
// added to it. This is a useful default which helps finding memory leaks.
var defaultMaxListeners = 10;
function checkListener(listener) {
    if (typeof listener !== 'function') {
        throw new TypeError('The "listener" argument must be of type Function. Received type ' + typeof listener);
    }
}
Object.defineProperty(EventEmitter, 'defaultMaxListeners', {
    enumerable: true,
    get: function () {
        return defaultMaxListeners;
    },
    set: function (arg) {
        if (typeof arg !== 'number' || arg < 0 || NumberIsNaN(arg)) {
            throw new RangeError('The value of "defaultMaxListeners" is out of range. It must be a non-negative number. Received ' + arg + '.');
        }
        defaultMaxListeners = arg;
    }
});
EventEmitter.init = function () {
    if (this._events === undefined ||
        this._events === Object.getPrototypeOf(this)._events) {
        this._events = Object.create(null);
        this._eventsCount = 0;
    }
    this._maxListeners = this._maxListeners || undefined;
};
// Obviously not all Emitters should be limited to 10. This function allows
// that to be increased. Set to zero for unlimited.
EventEmitter.prototype.setMaxListeners = function setMaxListeners(n) {
    if (typeof n !== 'number' || n < 0 || NumberIsNaN(n)) {
        throw new RangeError('The value of "n" is out of range. It must be a non-negative number. Received ' + n + '.');
    }
    this._maxListeners = n;
    return this;
};
function _getMaxListeners(that) {
    if (that._maxListeners === undefined)
        return EventEmitter.defaultMaxListeners;
    return that._maxListeners;
}
EventEmitter.prototype.getMaxListeners = function getMaxListeners() {
    return _getMaxListeners(this);
};
EventEmitter.prototype.emit = function emit(type) {
    var args = [];
    for (var i = 1; i < arguments.length; i++)
        args.push(arguments[i]);
    var doError = (type === 'error');
    var events = this._events;
    if (events !== undefined)
        doError = (doError && events.error === undefined);
    else if (!doError)
        return false;
    // If there is no 'error' event listener then throw.
    if (doError) {
        var er;
        if (args.length > 0)
            er = args[0];
        if (er instanceof Error) {
            // Note: The comments on the `throw` lines are intentional, they show
            // up in Node's output if this results in an unhandled exception.
            throw er; // Unhandled 'error' event
        }
        // At least give some kind of context to the user
        var err = new Error('Unhandled error.' + (er ? ' (' + er.message + ')' : ''));
        err.context = er;
        throw err; // Unhandled 'error' event
    }
    var handler = events[type];
    if (handler === undefined)
        return false;
    if (typeof handler === 'function') {
        ReflectApply(handler, this, args);
    }
    else {
        var len = handler.length;
        var listeners = arrayClone(handler, len);
        for (var i = 0; i < len; ++i)
            ReflectApply(listeners[i], this, args);
    }
    return true;
};
function _addListener(target, type, listener, prepend) {
    var m;
    var events;
    var existing;
    checkListener(listener);
    events = target._events;
    if (events === undefined) {
        events = target._events = Object.create(null);
        target._eventsCount = 0;
    }
    else {
        // To avoid recursion in the case that type === "newListener"! Before
        // adding it to the listeners, first emit "newListener".
        if (events.newListener !== undefined) {
            target.emit('newListener', type, listener.listener ? listener.listener : listener);
            // Re-assign `events` because a newListener handler could have caused the
            // this._events to be assigned to a new object
            events = target._events;
        }
        existing = events[type];
    }
    if (existing === undefined) {
        // Optimize the case of one listener. Don't need the extra array object.
        existing = events[type] = listener;
        ++target._eventsCount;
    }
    else {
        if (typeof existing === 'function') {
            // Adding the second element, need to change to array.
            existing = events[type] =
                prepend ? [listener, existing] : [existing, listener];
            // If we've already got an array, just append.
        }
        else if (prepend) {
            existing.unshift(listener);
        }
        else {
            existing.push(listener);
        }
        // Check for listener leak
        m = _getMaxListeners(target);
        if (m > 0 && existing.length > m && !existing.warned) {
            existing.warned = true;
            // No error code for this since it is a Warning
            // eslint-disable-next-line no-restricted-syntax
            var w = new Error('Possible EventEmitter memory leak detected. ' +
                existing.length + ' ' + String(type) + ' listeners ' +
                'added. Use emitter.setMaxListeners() to ' +
                'increase limit');
            w.name = 'MaxListenersExceededWarning';
            w.emitter = target;
            w.type = type;
            w.count = existing.length;
            ProcessEmitWarning(w);
        }
    }
    return target;
}
EventEmitter.prototype.addListener = function addListener(type, listener) {
    return _addListener(this, type, listener, false);
};
EventEmitter.prototype.on = EventEmitter.prototype.addListener;
EventEmitter.prototype.prependListener =
    function prependListener(type, listener) {
        return _addListener(this, type, listener, true);
    };
function onceWrapper() {
    if (!this.fired) {
        this.target.removeListener(this.type, this.wrapFn);
        this.fired = true;
        if (arguments.length === 0)
            return this.listener.call(this.target);
        return this.listener.apply(this.target, arguments);
    }
}
function _onceWrap(target, type, listener) {
    var state = { fired: false, wrapFn: undefined, target: target, type: type, listener: listener };
    var wrapped = onceWrapper.bind(state);
    wrapped.listener = listener;
    state.wrapFn = wrapped;
    return wrapped;
}
EventEmitter.prototype.once = function once(type, listener) {
    checkListener(listener);
    this.on(type, _onceWrap(this, type, listener));
    return this;
};
EventEmitter.prototype.prependOnceListener =
    function prependOnceListener(type, listener) {
        checkListener(listener);
        this.prependListener(type, _onceWrap(this, type, listener));
        return this;
    };
// Emits a 'removeListener' event if and only if the listener was removed.
EventEmitter.prototype.removeListener =
    function removeListener(type, listener) {
        var list, events, position, i, originalListener;
        checkListener(listener);
        events = this._events;
        if (events === undefined)
            return this;
        list = events[type];
        if (list === undefined)
            return this;
        if (list === listener || list.listener === listener) {
            if (--this._eventsCount === 0)
                this._events = Object.create(null);
            else {
                delete events[type];
                if (events.removeListener)
                    this.emit('removeListener', type, list.listener || listener);
            }
        }
        else if (typeof list !== 'function') {
            position = -1;
            for (i = list.length - 1; i >= 0; i--) {
                if (list[i] === listener || list[i].listener === listener) {
                    originalListener = list[i].listener;
                    position = i;
                    break;
                }
            }
            if (position < 0)
                return this;
            if (position === 0)
                list.shift();
            else {
                spliceOne(list, position);
            }
            if (list.length === 1)
                events[type] = list[0];
            if (events.removeListener !== undefined)
                this.emit('removeListener', type, originalListener || listener);
        }
        return this;
    };
EventEmitter.prototype.off = EventEmitter.prototype.removeListener;
EventEmitter.prototype.removeAllListeners =
    function removeAllListeners(type) {
        var listeners, events, i;
        events = this._events;
        if (events === undefined)
            return this;
        // not listening for removeListener, no need to emit
        if (events.removeListener === undefined) {
            if (arguments.length === 0) {
                this._events = Object.create(null);
                this._eventsCount = 0;
            }
            else if (events[type] !== undefined) {
                if (--this._eventsCount === 0)
                    this._events = Object.create(null);
                else
                    delete events[type];
            }
            return this;
        }
        // emit removeListener for all listeners on all events
        if (arguments.length === 0) {
            var keys = Object.keys(events);
            var key;
            for (i = 0; i < keys.length; ++i) {
                key = keys[i];
                if (key === 'removeListener')
                    continue;
                this.removeAllListeners(key);
            }
            this.removeAllListeners('removeListener');
            this._events = Object.create(null);
            this._eventsCount = 0;
            return this;
        }
        listeners = events[type];
        if (typeof listeners === 'function') {
            this.removeListener(type, listeners);
        }
        else if (listeners !== undefined) {
            // LIFO order
            for (i = listeners.length - 1; i >= 0; i--) {
                this.removeListener(type, listeners[i]);
            }
        }
        return this;
    };
function _listeners(target, type, unwrap) {
    var events = target._events;
    if (events === undefined)
        return [];
    var evlistener = events[type];
    if (evlistener === undefined)
        return [];
    if (typeof evlistener === 'function')
        return unwrap ? [evlistener.listener || evlistener] : [evlistener];
    return unwrap ?
        unwrapListeners(evlistener) : arrayClone(evlistener, evlistener.length);
}
EventEmitter.prototype.listeners = function listeners(type) {
    return _listeners(this, type, true);
};
EventEmitter.prototype.rawListeners = function rawListeners(type) {
    return _listeners(this, type, false);
};
EventEmitter.listenerCount = function (emitter, type) {
    if (typeof emitter.listenerCount === 'function') {
        return emitter.listenerCount(type);
    }
    else {
        return listenerCount.call(emitter, type);
    }
};
EventEmitter.prototype.listenerCount = listenerCount;
function listenerCount(type) {
    var events = this._events;
    if (events !== undefined) {
        var evlistener = events[type];
        if (typeof evlistener === 'function') {
            return 1;
        }
        else if (evlistener !== undefined) {
            return evlistener.length;
        }
    }
    return 0;
}
EventEmitter.prototype.eventNames = function eventNames() {
    return this._eventsCount > 0 ? ReflectOwnKeys(this._events) : [];
};
function arrayClone(arr, n) {
    var copy = new Array(n);
    for (var i = 0; i < n; ++i)
        copy[i] = arr[i];
    return copy;
}
function spliceOne(list, index) {
    for (; index + 1 < list.length; index++)
        list[index] = list[index + 1];
    list.pop();
}
function unwrapListeners(arr) {
    var ret = new Array(arr.length);
    for (var i = 0; i < ret.length; ++i) {
        ret[i] = arr[i].listener || arr[i];
    }
    return ret;
}
function once(emitter, name) {
    return new Promise(function (resolve, reject) {
        function errorListener(err) {
            emitter.removeListener(name, resolver);
            reject(err);
        }
        function resolver() {
            if (typeof emitter.removeListener === 'function') {
                emitter.removeListener('error', errorListener);
            }
            resolve([].slice.call(arguments));
        }
        ;
        eventTargetAgnosticAddListener(emitter, name, resolver, { once: true });
        if (name !== 'error') {
            addErrorHandlerIfEventEmitter(emitter, errorListener, { once: true });
        }
    });
}
function addErrorHandlerIfEventEmitter(emitter, handler, flags) {
    if (typeof emitter.on === 'function') {
        eventTargetAgnosticAddListener(emitter, 'error', handler, flags);
    }
}
function eventTargetAgnosticAddListener(emitter, name, listener, flags) {
    if (typeof emitter.on === 'function') {
        if (flags.once) {
            emitter.once(name, listener);
        }
        else {
            emitter.on(name, listener);
        }
    }
    else if (typeof emitter.addEventListener === 'function') {
        // EventTarget does not have `error` event semantics like Node
        // EventEmitters, we do not listen for `error` events here.
        emitter.addEventListener(name, function wrapListener(arg) {
            // IE does not have builtin `{ once: true }` support so we
            // have to do it manually.
            if (flags.once) {
                emitter.removeEventListener(name, wrapListener);
            }
            listener(arg);
        });
    }
    else {
        throw new TypeError('The "emitter" argument must be of type EventEmitter. Received type ' + typeof emitter);
    }
}


/***/ }),

/***/ "./node_modules/ieee754/index.js":
/*!***************************************!*\
  !*** ./node_modules/ieee754/index.js ***!
  \***************************************/
/*! no static exports found */
/***/ (function(module, exports) {

/*! ieee754. BSD-3-Clause License. Feross Aboukhadijeh <https://feross.org/opensource> */
exports.read = function (buffer, offset, isLE, mLen, nBytes) {
    var e, m;
    var eLen = (nBytes * 8) - mLen - 1;
    var eMax = (1 << eLen) - 1;
    var eBias = eMax >> 1;
    var nBits = -7;
    var i = isLE ? (nBytes - 1) : 0;
    var d = isLE ? -1 : 1;
    var s = buffer[offset + i];
    i += d;
    e = s & ((1 << (-nBits)) - 1);
    s >>= (-nBits);
    nBits += eLen;
    for (; nBits > 0; e = (e * 256) + buffer[offset + i], i += d, nBits -= 8) { }
    m = e & ((1 << (-nBits)) - 1);
    e >>= (-nBits);
    nBits += mLen;
    for (; nBits > 0; m = (m * 256) + buffer[offset + i], i += d, nBits -= 8) { }
    if (e === 0) {
        e = 1 - eBias;
    }
    else if (e === eMax) {
        return m ? NaN : ((s ? -1 : 1) * Infinity);
    }
    else {
        m = m + Math.pow(2, mLen);
        e = e - eBias;
    }
    return (s ? -1 : 1) * m * Math.pow(2, e - mLen);
};
exports.write = function (buffer, value, offset, isLE, mLen, nBytes) {
    var e, m, c;
    var eLen = (nBytes * 8) - mLen - 1;
    var eMax = (1 << eLen) - 1;
    var eBias = eMax >> 1;
    var rt = (mLen === 23 ? Math.pow(2, -24) - Math.pow(2, -77) : 0);
    var i = isLE ? 0 : (nBytes - 1);
    var d = isLE ? 1 : -1;
    var s = value < 0 || (value === 0 && 1 / value < 0) ? 1 : 0;
    value = Math.abs(value);
    if (isNaN(value) || value === Infinity) {
        m = isNaN(value) ? 1 : 0;
        e = eMax;
    }
    else {
        e = Math.floor(Math.log(value) / Math.LN2);
        if (value * (c = Math.pow(2, -e)) < 1) {
            e--;
            c *= 2;
        }
        if (e + eBias >= 1) {
            value += rt / c;
        }
        else {
            value += rt * Math.pow(2, 1 - eBias);
        }
        if (value * c >= 2) {
            e++;
            c /= 2;
        }
        if (e + eBias >= eMax) {
            m = 0;
            e = eMax;
        }
        else if (e + eBias >= 1) {
            m = ((value * c) - 1) * Math.pow(2, mLen);
            e = e + eBias;
        }
        else {
            m = value * Math.pow(2, eBias - 1) * Math.pow(2, mLen);
            e = 0;
        }
    }
    for (; mLen >= 8; buffer[offset + i] = m & 0xff, i += d, m /= 256, mLen -= 8) { }
    e = (e << mLen) | m;
    eLen += mLen;
    for (; eLen > 0; buffer[offset + i] = e & 0xff, i += d, e /= 256, eLen -= 8) { }
    buffer[offset + i - d] |= s * 128;
};


/***/ }),

/***/ "./node_modules/process/browser.js":
/*!*****************************************!*\
  !*** ./node_modules/process/browser.js ***!
  \*****************************************/
/*! no static exports found */
/***/ (function(module, exports) {

// shim for using process in browser
var process = module.exports = {};
// cached from whatever global is present so that test runners that stub it
// don't break things.  But we need to wrap it in a try catch in case it is
// wrapped in strict mode code which doesn't define any globals.  It's inside a
// function because try/catches deoptimize in certain engines.
var cachedSetTimeout;
var cachedClearTimeout;
function defaultSetTimout() {
    throw new Error('setTimeout has not been defined');
}
function defaultClearTimeout() {
    throw new Error('clearTimeout has not been defined');
}
(function () {
    try {
        if (typeof setTimeout === 'function') {
            cachedSetTimeout = setTimeout;
        }
        else {
            cachedSetTimeout = defaultSetTimout;
        }
    }
    catch (e) {
        cachedSetTimeout = defaultSetTimout;
    }
    try {
        if (typeof clearTimeout === 'function') {
            cachedClearTimeout = clearTimeout;
        }
        else {
            cachedClearTimeout = defaultClearTimeout;
        }
    }
    catch (e) {
        cachedClearTimeout = defaultClearTimeout;
    }
}());
function runTimeout(fun) {
    if (cachedSetTimeout === setTimeout) {
        //normal enviroments in sane situations
        return setTimeout(fun, 0);
    }
    // if setTimeout wasn't available but was latter defined
    if ((cachedSetTimeout === defaultSetTimout || !cachedSetTimeout) && setTimeout) {
        cachedSetTimeout = setTimeout;
        return setTimeout(fun, 0);
    }
    try {
        // when when somebody has screwed with setTimeout but no I.E. maddness
        return cachedSetTimeout(fun, 0);
    }
    catch (e) {
        try {
            // When we are in I.E. but the script has been evaled so I.E. doesn't trust the global object when called normally
            return cachedSetTimeout.call(null, fun, 0);
        }
        catch (e) {
            // same as above but when it's a version of I.E. that must have the global object for 'this', hopfully our context correct otherwise it will throw a global error
            return cachedSetTimeout.call(this, fun, 0);
        }
    }
}
function runClearTimeout(marker) {
    if (cachedClearTimeout === clearTimeout) {
        //normal enviroments in sane situations
        return clearTimeout(marker);
    }
    // if clearTimeout wasn't available but was latter defined
    if ((cachedClearTimeout === defaultClearTimeout || !cachedClearTimeout) && clearTimeout) {
        cachedClearTimeout = clearTimeout;
        return clearTimeout(marker);
    }
    try {
        // when when somebody has screwed with setTimeout but no I.E. maddness
        return cachedClearTimeout(marker);
    }
    catch (e) {
        try {
            // When we are in I.E. but the script has been evaled so I.E. doesn't  trust the global object when called normally
            return cachedClearTimeout.call(null, marker);
        }
        catch (e) {
            // same as above but when it's a version of I.E. that must have the global object for 'this', hopfully our context correct otherwise it will throw a global error.
            // Some versions of I.E. have different rules for clearTimeout vs setTimeout
            return cachedClearTimeout.call(this, marker);
        }
    }
}
var queue = [];
var draining = false;
var currentQueue;
var queueIndex = -1;
function cleanUpNextTick() {
    if (!draining || !currentQueue) {
        return;
    }
    draining = false;
    if (currentQueue.length) {
        queue = currentQueue.concat(queue);
    }
    else {
        queueIndex = -1;
    }
    if (queue.length) {
        drainQueue();
    }
}
function drainQueue() {
    if (draining) {
        return;
    }
    var timeout = runTimeout(cleanUpNextTick);
    draining = true;
    var len = queue.length;
    while (len) {
        currentQueue = queue;
        queue = [];
        while (++queueIndex < len) {
            if (currentQueue) {
                currentQueue[queueIndex].run();
            }
        }
        queueIndex = -1;
        len = queue.length;
    }
    currentQueue = null;
    draining = false;
    runClearTimeout(timeout);
}
process.nextTick = function (fun) {
    var args = new Array(arguments.length - 1);
    if (arguments.length > 1) {
        for (var i = 1; i < arguments.length; i++) {
            args[i - 1] = arguments[i];
        }
    }
    queue.push(new Item(fun, args));
    if (queue.length === 1 && !draining) {
        runTimeout(drainQueue);
    }
};
// v8 likes predictible objects
function Item(fun, array) {
    this.fun = fun;
    this.array = array;
}
Item.prototype.run = function () {
    this.fun.apply(null, this.array);
};
process.title = 'browser';
process.browser = true;
process.env = {};
process.argv = [];
process.version = ''; // empty string to avoid regexp issues
process.versions = {};
function noop() { }
process.on = noop;
process.addListener = noop;
process.once = noop;
process.off = noop;
process.removeListener = noop;
process.removeAllListeners = noop;
process.emit = noop;
process.prependListener = noop;
process.prependOnceListener = noop;
process.listeners = function (name) { return []; };
process.binding = function (name) {
    throw new Error('process.binding is not supported');
};
process.cwd = function () { return '/'; };
process.chdir = function (dir) {
    throw new Error('process.chdir is not supported');
};
process.umask = function () { return 0; };


/***/ }),

/***/ "./node_modules/webpack/buildin/global.js":
/*!***********************************!*\
  !*** (webpack)/buildin/global.js ***!
  \***********************************/
/*! no static exports found */
/***/ (function(module, exports) {

var g;
// This works in non-strict mode
g = (function () {
    return this;
})();
try {
    // This works if eval is allowed (see CSP)
    g = g || new Function("return this")();
}
catch (e) {
    // This works if the window reference is available
    if (typeof window === "object")
        g = window;
}
// g can still be undefined, but nothing to do about it...
// We return undefined, instead of nothing here, so it's
// easier to handle this case. if(!global) { ...}
module.exports = g;


/***/ }),

/***/ "./src/config.js":
/*!***********************!*\
  !*** ./src/config.js ***!
  \***********************/
/*! exports provided: defaultConfig, createDefaultConfig */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "defaultConfig", function() { return defaultConfig; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "createDefaultConfig", function() { return createDefaultConfig; });
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var defaultConfig = {
    enableWorker: false,
    enableWorkerForMSE: false,
    enableStashBuffer: true,
    stashInitialSize: undefined,
    isLive: false,
    liveBufferLatencyChasing: false,
    liveBufferLatencyChasingOnPaused: false,
    liveBufferLatencyMaxLatency: 1.5,
    liveBufferLatencyMinRemain: 0.5,
    liveSync: false,
    liveSyncMaxLatency: 1.2,
    liveSyncTargetLatency: 0.8,
    liveSyncPlaybackRate: 1.2,
    lazyLoad: true,
    lazyLoadMaxDuration: 3 * 60,
    lazyLoadRecoverDuration: 30,
    deferLoadAfterSourceOpen: true,
    // autoCleanupSourceBuffer: default as false, leave unspecified
    autoCleanupMaxBackwardDuration: 3 * 60,
    autoCleanupMinBackwardDuration: 2 * 60,
    statisticsInfoReportInterval: 600,
    fixAudioTimestampGap: true,
    accurateSeek: false,
    seekType: 'range',
    seekParamStart: 'bstart',
    seekParamEnd: 'bend',
    rangeLoadZeroStart: false,
    customSeekHandler: undefined,
    reuseRedirectedURL: false,
    // referrerPolicy: leave as unspecified
    headers: undefined,
    customLoader: undefined
};
function createDefaultConfig() {
    return Object.assign({}, defaultConfig);
}


/***/ }),

/***/ "./src/core/features.js":
/*!******************************!*\
  !*** ./src/core/features.js ***!
  \******************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _io_io_controller_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../io/io-controller.js */ "./src/io/io-controller.js");
/* harmony import */ var _config_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ../config.js */ "./src/config.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */


var Features = /** @class */ (function () {
    function Features() {
    }
    Features.supportMSEH264Playback = function () {
        var avc_aac_mime_type = 'video/mp4; codecs="avc1.42E01E,mp4a.40.2"';
        var support_w3c_mse = self.MediaSource && self.MediaSource.isTypeSupported(avc_aac_mime_type);
        var support_apple_mme = self.ManagedMediaSource && self.ManagedMediaSource.isTypeSupported(avc_aac_mime_type);
        return support_w3c_mse || support_apple_mme;
    };
    Features.supportMSEH265Playback = function () {
        var hevc_mime_type = 'video/mp4; codecs="hvc1.1.6.L93.B0"';
        var support_w3c_mse = self.MediaSource && self.MediaSource.isTypeSupported(hevc_mime_type);
        var support_apple_mme = self.ManagedMediaSource && self.ManagedMediaSource.isTypeSupported(hevc_mime_type);
        return support_w3c_mse || support_apple_mme;
    };
    Features.supportNetworkStreamIO = function () {
        var ioctl = new _io_io_controller_js__WEBPACK_IMPORTED_MODULE_0__["default"]({}, Object(_config_js__WEBPACK_IMPORTED_MODULE_1__["createDefaultConfig"])());
        var loaderType = ioctl.loaderType;
        ioctl.destroy();
        return loaderType == 'fetch-stream-loader' || loaderType == 'xhr-moz-chunked-loader';
    };
    Features.getNetworkLoaderTypeName = function () {
        var ioctl = new _io_io_controller_js__WEBPACK_IMPORTED_MODULE_0__["default"]({}, Object(_config_js__WEBPACK_IMPORTED_MODULE_1__["createDefaultConfig"])());
        var loaderType = ioctl.loaderType;
        ioctl.destroy();
        return loaderType;
    };
    Features.supportNativeMediaPlayback = function (mimeType) {
        if (Features.videoElement == undefined) {
            Features.videoElement = window.document.createElement('video');
        }
        var canPlay = Features.videoElement.canPlayType(mimeType);
        return canPlay === 'probably' || canPlay == 'maybe';
    };
    Features.getFeatureList = function () {
        var features = {
            msePlayback: false,
            mseLivePlayback: false,
            mseH265Playback: false,
            networkStreamIO: false,
            networkLoaderName: '',
            nativeMP4H264Playback: false,
            nativeMP4H265Playback: false,
            nativeWebmVP8Playback: false,
            nativeWebmVP9Playback: false
        };
        features.msePlayback = Features.supportMSEH264Playback();
        features.networkStreamIO = Features.supportNetworkStreamIO();
        features.networkLoaderName = Features.getNetworkLoaderTypeName();
        features.mseLivePlayback = features.msePlayback && features.networkStreamIO;
        features.mseH265Playback = Features.supportMSEH265Playback();
        features.nativeMP4H264Playback = Features.supportNativeMediaPlayback('video/mp4; codecs="avc1.42001E, mp4a.40.2"');
        features.nativeMP4H265Playback = Features.supportNativeMediaPlayback('video/mp4; codecs="hvc1.1.6.L93.B0"');
        features.nativeWebmVP8Playback = Features.supportNativeMediaPlayback('video/webm; codecs="vp8.0, vorbis"');
        features.nativeWebmVP9Playback = Features.supportNativeMediaPlayback('video/webm; codecs="vp9"');
        return features;
    };
    return Features;
}());
/* harmony default export */ __webpack_exports__["default"] = (Features);


/***/ }),

/***/ "./src/core/media-info.js":
/*!********************************!*\
  !*** ./src/core/media-info.js ***!
  \********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var MediaInfo = /** @class */ (function () {
    function MediaInfo() {
        this.mimeType = null;
        this.duration = null;
        this.hasAudio = null;
        this.hasVideo = null;
        this.audioCodec = null;
        this.videoCodec = null;
        this.audioDataRate = null;
        this.videoDataRate = null;
        this.audioSampleRate = null;
        this.audioChannelCount = null;
        this.width = null;
        this.height = null;
        this.fps = null;
        this.profile = null;
        this.level = null;
        this.refFrames = null;
        this.chromaFormat = null;
        this.sarNum = null;
        this.sarDen = null;
        this.metadata = null;
        this.segments = null; // MediaInfo[]
        this.segmentCount = null;
        this.hasKeyframesIndex = null;
        this.keyframesIndex = null;
    }
    MediaInfo.prototype.isComplete = function () {
        var audioInfoComplete = (this.hasAudio === false) ||
            (this.hasAudio === true &&
                this.audioCodec != null &&
                this.audioSampleRate != null &&
                this.audioChannelCount != null);
        var videoInfoComplete = (this.hasVideo === false) ||
            (this.hasVideo === true &&
                this.videoCodec != null &&
                this.width != null &&
                this.height != null &&
                this.fps != null &&
                this.profile != null &&
                this.level != null &&
                this.refFrames != null &&
                this.chromaFormat != null &&
                this.sarNum != null &&
                this.sarDen != null);
        // keyframesIndex may not be present
        return this.mimeType != null &&
            audioInfoComplete &&
            videoInfoComplete;
    };
    MediaInfo.prototype.isSeekable = function () {
        return this.hasKeyframesIndex === true;
    };
    MediaInfo.prototype.getNearestKeyframe = function (milliseconds) {
        if (this.keyframesIndex == null) {
            return null;
        }
        var table = this.keyframesIndex;
        var keyframeIdx = this._search(table.times, milliseconds);
        return {
            index: keyframeIdx,
            milliseconds: table.times[keyframeIdx],
            fileposition: table.filepositions[keyframeIdx]
        };
    };
    MediaInfo.prototype._search = function (list, value) {
        var idx = 0;
        var last = list.length - 1;
        var mid = 0;
        var lbound = 0;
        var ubound = last;
        if (value < list[0]) {
            idx = 0;
            lbound = ubound + 1; // skip search
        }
        while (lbound <= ubound) {
            mid = lbound + Math.floor((ubound - lbound) / 2);
            if (mid === last || (value >= list[mid] && value < list[mid + 1])) {
                idx = mid;
                break;
            }
            else if (list[mid] < value) {
                lbound = mid + 1;
            }
            else {
                ubound = mid - 1;
            }
        }
        return idx;
    };
    return MediaInfo;
}());
/* harmony default export */ __webpack_exports__["default"] = (MediaInfo);


/***/ }),

/***/ "./src/core/media-segment-info.js":
/*!****************************************!*\
  !*** ./src/core/media-segment-info.js ***!
  \****************************************/
/*! exports provided: SampleInfo, MediaSegmentInfo, IDRSampleList, MediaSegmentInfoList */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "SampleInfo", function() { return SampleInfo; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "MediaSegmentInfo", function() { return MediaSegmentInfo; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "IDRSampleList", function() { return IDRSampleList; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "MediaSegmentInfoList", function() { return MediaSegmentInfoList; });
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
// Represents an media sample (audio / video)
var SampleInfo = /** @class */ (function () {
    function SampleInfo(dts, pts, duration, originalDts, isSync) {
        this.dts = dts;
        this.pts = pts;
        this.duration = duration;
        this.originalDts = originalDts;
        this.isSyncPoint = isSync;
        this.fileposition = null;
    }
    return SampleInfo;
}());

// Media Segment concept is defined in Media Source Extensions spec.
// Particularly in ISO BMFF format, an Media Segment contains a moof box followed by a mdat box.
var MediaSegmentInfo = /** @class */ (function () {
    function MediaSegmentInfo() {
        this.beginDts = 0;
        this.endDts = 0;
        this.beginPts = 0;
        this.endPts = 0;
        this.originalBeginDts = 0;
        this.originalEndDts = 0;
        this.syncPoints = []; // SampleInfo[n], for video IDR frames only
        this.firstSample = null; // SampleInfo
        this.lastSample = null; // SampleInfo
    }
    MediaSegmentInfo.prototype.appendSyncPoint = function (sampleInfo) {
        sampleInfo.isSyncPoint = true;
        this.syncPoints.push(sampleInfo);
    };
    return MediaSegmentInfo;
}());

// Ordered list for recording video IDR frames, sorted by originalDts
var IDRSampleList = /** @class */ (function () {
    function IDRSampleList() {
        this._list = [];
    }
    IDRSampleList.prototype.clear = function () {
        this._list = [];
    };
    IDRSampleList.prototype.appendArray = function (syncPoints) {
        var list = this._list;
        if (syncPoints.length === 0) {
            return;
        }
        if (list.length > 0 && syncPoints[0].originalDts < list[list.length - 1].originalDts) {
            this.clear();
        }
        Array.prototype.push.apply(list, syncPoints);
    };
    IDRSampleList.prototype.getLastSyncPointBeforeDts = function (dts) {
        if (this._list.length == 0) {
            return null;
        }
        var list = this._list;
        var idx = 0;
        var last = list.length - 1;
        var mid = 0;
        var lbound = 0;
        var ubound = last;
        if (dts < list[0].dts) {
            idx = 0;
            lbound = ubound + 1;
        }
        while (lbound <= ubound) {
            mid = lbound + Math.floor((ubound - lbound) / 2);
            if (mid === last || (dts >= list[mid].dts && dts < list[mid + 1].dts)) {
                idx = mid;
                break;
            }
            else if (list[mid].dts < dts) {
                lbound = mid + 1;
            }
            else {
                ubound = mid - 1;
            }
        }
        return this._list[idx];
    };
    return IDRSampleList;
}());

// Data structure for recording information of media segments in single track.
var MediaSegmentInfoList = /** @class */ (function () {
    function MediaSegmentInfoList(type) {
        this._type = type;
        this._list = [];
        this._lastAppendLocation = -1; // cached last insert location
    }
    Object.defineProperty(MediaSegmentInfoList.prototype, "type", {
        get: function () {
            return this._type;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(MediaSegmentInfoList.prototype, "length", {
        get: function () {
            return this._list.length;
        },
        enumerable: false,
        configurable: true
    });
    MediaSegmentInfoList.prototype.isEmpty = function () {
        return this._list.length === 0;
    };
    MediaSegmentInfoList.prototype.clear = function () {
        this._list = [];
        this._lastAppendLocation = -1;
    };
    MediaSegmentInfoList.prototype._searchNearestSegmentBefore = function (originalBeginDts) {
        var list = this._list;
        if (list.length === 0) {
            return -2;
        }
        var last = list.length - 1;
        var mid = 0;
        var lbound = 0;
        var ubound = last;
        var idx = 0;
        if (originalBeginDts < list[0].originalBeginDts) {
            idx = -1;
            return idx;
        }
        while (lbound <= ubound) {
            mid = lbound + Math.floor((ubound - lbound) / 2);
            if (mid === last || (originalBeginDts > list[mid].lastSample.originalDts &&
                (originalBeginDts < list[mid + 1].originalBeginDts))) {
                idx = mid;
                break;
            }
            else if (list[mid].originalBeginDts < originalBeginDts) {
                lbound = mid + 1;
            }
            else {
                ubound = mid - 1;
            }
        }
        return idx;
    };
    MediaSegmentInfoList.prototype._searchNearestSegmentAfter = function (originalBeginDts) {
        return this._searchNearestSegmentBefore(originalBeginDts) + 1;
    };
    MediaSegmentInfoList.prototype.append = function (mediaSegmentInfo) {
        var list = this._list;
        var msi = mediaSegmentInfo;
        var lastAppendIdx = this._lastAppendLocation;
        var insertIdx = 0;
        if (lastAppendIdx !== -1 && lastAppendIdx < list.length &&
            msi.originalBeginDts >= list[lastAppendIdx].lastSample.originalDts &&
            ((lastAppendIdx === list.length - 1) ||
                (lastAppendIdx < list.length - 1 &&
                    msi.originalBeginDts < list[lastAppendIdx + 1].originalBeginDts))) {
            insertIdx = lastAppendIdx + 1; // use cached location idx
        }
        else {
            if (list.length > 0) {
                insertIdx = this._searchNearestSegmentBefore(msi.originalBeginDts) + 1;
            }
        }
        this._lastAppendLocation = insertIdx;
        this._list.splice(insertIdx, 0, msi);
    };
    MediaSegmentInfoList.prototype.getLastSegmentBefore = function (originalBeginDts) {
        var idx = this._searchNearestSegmentBefore(originalBeginDts);
        if (idx >= 0) {
            return this._list[idx];
        }
        else { // -1
            return null;
        }
    };
    MediaSegmentInfoList.prototype.getLastSampleBefore = function (originalBeginDts) {
        var segment = this.getLastSegmentBefore(originalBeginDts);
        if (segment != null) {
            return segment.lastSample;
        }
        else {
            return null;
        }
    };
    MediaSegmentInfoList.prototype.getLastSyncPointBefore = function (originalBeginDts) {
        var segmentIdx = this._searchNearestSegmentBefore(originalBeginDts);
        var syncPoints = this._list[segmentIdx].syncPoints;
        while (syncPoints.length === 0 && segmentIdx > 0) {
            segmentIdx--;
            syncPoints = this._list[segmentIdx].syncPoints;
        }
        if (syncPoints.length > 0) {
            return syncPoints[syncPoints.length - 1];
        }
        else {
            return null;
        }
    };
    return MediaSegmentInfoList;
}());



/***/ }),

/***/ "./src/core/mse-controller.js":
/*!************************************!*\
  !*** ./src/core/mse-controller.js ***!
  \************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! events */ "./node_modules/events/events.js");
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0___default = /*#__PURE__*/__webpack_require__.n(events__WEBPACK_IMPORTED_MODULE_0__);
/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/* harmony import */ var _utils_browser_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ../utils/browser.js */ "./src/utils/browser.js");
/* harmony import */ var _mse_events__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ./mse-events */ "./src/core/mse-events.ts");
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_4__ = __webpack_require__(/*! ../utils/exception.js */ "./src/utils/exception.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */





// Media Source Extensions controller
var MSEController = /** @class */ (function () {
    function MSEController(config) {
        this.TAG = 'MSEController';
        this._config = config;
        this._emitter = new events__WEBPACK_IMPORTED_MODULE_0___default.a();
        if (this._config.isLive && this._config.autoCleanupSourceBuffer == undefined) {
            // For live stream, do auto cleanup by default
            this._config.autoCleanupSourceBuffer = true;
        }
        this.e = {
            onSourceOpen: this._onSourceOpen.bind(this),
            onSourceEnded: this._onSourceEnded.bind(this),
            onSourceClose: this._onSourceClose.bind(this),
            onStartStreaming: this._onStartStreaming.bind(this),
            onEndStreaming: this._onEndStreaming.bind(this),
            onQualityChange: this._onQualityChange.bind(this),
            onSourceBufferError: this._onSourceBufferError.bind(this),
            onSourceBufferUpdateEnd: this._onSourceBufferUpdateEnd.bind(this)
        };
        // Use ManagedMediaSource only if w3c MediaSource is not available (e.g. iOS Safari)
        this._useManagedMediaSource = ('ManagedMediaSource' in self) && !('MediaSource' in self);
        this._mediaSource = null;
        this._mediaSourceObjectURL = null;
        this._mediaElementProxy = null;
        this._isBufferFull = false;
        this._hasPendingEos = false;
        this._requireSetMediaDuration = false;
        this._pendingMediaDuration = 0;
        this._pendingSourceBufferInit = [];
        this._mimeTypes = {
            video: null,
            audio: null
        };
        this._sourceBuffers = {
            video: null,
            audio: null
        };
        this._lastInitSegments = {
            video: null,
            audio: null
        };
        this._pendingSegments = {
            video: [],
            audio: []
        };
        this._pendingRemoveRanges = {
            video: [],
            audio: []
        };
    }
    MSEController.prototype.destroy = function () {
        if (this._mediaSource) {
            this.shutdown();
        }
        if (this._mediaSourceObjectURL) {
            this.revokeObjectURL();
        }
        this.e = null;
        this._emitter.removeAllListeners();
        this._emitter = null;
    };
    MSEController.prototype.on = function (event, listener) {
        this._emitter.addListener(event, listener);
    };
    MSEController.prototype.off = function (event, listener) {
        this._emitter.removeListener(event, listener);
    };
    MSEController.prototype.initialize = function (mediaElementProxy) {
        if (this._mediaSource) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_4__["IllegalStateException"]('MediaSource has been attached to an HTMLMediaElement!');
        }
        if (this._useManagedMediaSource) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].v(this.TAG, 'Using ManagedMediaSource');
        }
        var ms = this._mediaSource = this._useManagedMediaSource ? new self.ManagedMediaSource() : new self.MediaSource();
        ms.addEventListener('sourceopen', this.e.onSourceOpen);
        ms.addEventListener('sourceended', this.e.onSourceEnded);
        ms.addEventListener('sourceclose', this.e.onSourceClose);
        if (this._useManagedMediaSource) {
            ms.addEventListener('startstreaming', this.e.onStartStreaming);
            ms.addEventListener('endstreaming', this.e.onEndStreaming);
            ms.addEventListener('qualitychange', this.e.onQualityChange);
        }
        this._mediaElementProxy = mediaElementProxy;
    };
    MSEController.prototype.shutdown = function () {
        if (this._mediaSource) {
            var ms = this._mediaSource;
            for (var type in this._sourceBuffers) {
                // pending segments should be discard
                var ps = this._pendingSegments[type];
                ps.splice(0, ps.length);
                this._pendingSegments[type] = null;
                this._pendingRemoveRanges[type] = null;
                this._lastInitSegments[type] = null;
                // remove all sourcebuffers
                var sb = this._sourceBuffers[type];
                if (sb) {
                    if (ms.readyState !== 'closed') {
                        // ms edge can throw an error: Unexpected call to method or property access
                        try {
                            ms.removeSourceBuffer(sb);
                        }
                        catch (error) {
                            _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].e(this.TAG, error.message);
                        }
                        sb.removeEventListener('error', this.e.onSourceBufferError);
                        sb.removeEventListener('updateend', this.e.onSourceBufferUpdateEnd);
                    }
                    this._mimeTypes[type] = null;
                    this._sourceBuffers[type] = null;
                }
            }
            if (ms.readyState === 'open') {
                try {
                    ms.endOfStream();
                }
                catch (error) {
                    _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].e(this.TAG, error.message);
                }
            }
            this._mediaElementProxy = null;
            ms.removeEventListener('sourceopen', this.e.onSourceOpen);
            ms.removeEventListener('sourceended', this.e.onSourceEnded);
            ms.removeEventListener('sourceclose', this.e.onSourceClose);
            if (this._useManagedMediaSource) {
                ms.removeEventListener('startstreaming', this.e.onStartStreaming);
                ms.removeEventListener('endstreaming', this.e.onEndStreaming);
                ms.removeEventListener('qualitychange', this.e.onQualityChange);
            }
            this._pendingSourceBufferInit = [];
            this._isBufferFull = false;
            this._mediaSource = null;
        }
    };
    MSEController.prototype.isManagedMediaSource = function () {
        return this._useManagedMediaSource;
    };
    MSEController.prototype.getObject = function () {
        if (!this._mediaSource) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_4__["IllegalStateException"]('MediaSource has not been initialized yet!');
        }
        return this._mediaSource;
    };
    MSEController.prototype.getHandle = function () {
        if (!this._mediaSource) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_4__["IllegalStateException"]('MediaSource has not been initialized yet!');
        }
        return this._mediaSource.handle;
    };
    MSEController.prototype.getObjectURL = function () {
        if (!this._mediaSource) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_4__["IllegalStateException"]('MediaSource has not been initialized yet!');
        }
        if (this._mediaSourceObjectURL == null) {
            this._mediaSourceObjectURL = URL.createObjectURL(this._mediaSource);
        }
        return this._mediaSourceObjectURL;
    };
    MSEController.prototype.revokeObjectURL = function () {
        if (this._mediaSourceObjectURL) {
            URL.revokeObjectURL(this._mediaSourceObjectURL);
            this._mediaSourceObjectURL = null;
        }
    };
    MSEController.prototype.appendInitSegment = function (initSegment, deferred) {
        if (deferred === void 0) { deferred = undefined; }
        if (!this._mediaSource || this._mediaSource.readyState !== 'open' || this._mediaSource.streaming === false) {
            // sourcebuffer creation requires mediaSource.readyState === 'open'
            // so we defer the sourcebuffer creation, until sourceopen event triggered
            this._pendingSourceBufferInit.push(initSegment);
            // make sure that this InitSegment is in the front of pending segments queue
            this._pendingSegments[initSegment.type].push(initSegment);
            return;
        }
        var is = initSegment;
        var mimeType = "".concat(is.container);
        if (is.codec && is.codec.length > 0) {
            if (is.codec === 'opus' && _utils_browser_js__WEBPACK_IMPORTED_MODULE_2__["default"].safari) {
                is.codec = 'Opus';
            }
            mimeType += ";codecs=".concat(is.codec);
        }
        var firstInitSegment = false;
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].v(this.TAG, 'Received Initialization Segment, mimeType: ' + mimeType);
        this._lastInitSegments[is.type] = is;
        if (mimeType !== this._mimeTypes[is.type]) {
            if (!this._mimeTypes[is.type]) { // empty, first chance create sourcebuffer
                firstInitSegment = true;
                try {
                    var sb = this._sourceBuffers[is.type] = this._mediaSource.addSourceBuffer(mimeType);
                    sb.addEventListener('error', this.e.onSourceBufferError);
                    sb.addEventListener('updateend', this.e.onSourceBufferUpdateEnd);
                }
                catch (error) {
                    _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].e(this.TAG, error.message);
                    this._emitter.emit(_mse_events__WEBPACK_IMPORTED_MODULE_3__["default"].ERROR, { code: error.code, msg: error.message });
                    return;
                }
            }
            else {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].v(this.TAG, "Notice: ".concat(is.type, " mimeType changed, origin: ").concat(this._mimeTypes[is.type], ", target: ").concat(mimeType));
            }
            this._mimeTypes[is.type] = mimeType;
        }
        if (!deferred) {
            // deferred means this InitSegment has been pushed to pendingSegments queue
            this._pendingSegments[is.type].push(is);
        }
        if (!firstInitSegment) { // append immediately only if init segment in subsequence
            if (this._sourceBuffers[is.type] && !this._sourceBuffers[is.type].updating) {
                this._doAppendSegments();
            }
        }
        if (_utils_browser_js__WEBPACK_IMPORTED_MODULE_2__["default"].safari && is.container === 'audio/mpeg' && is.mediaDuration > 0) {
            // 'audio/mpeg' track under Safari may cause MediaElement's duration to be NaN
            // Manually correct MediaSource.duration to make progress bar seekable, and report right duration
            this._requireSetMediaDuration = true;
            this._pendingMediaDuration = is.mediaDuration / 1000; // in seconds
            this._updateMediaSourceDuration();
        }
    };
    MSEController.prototype.appendMediaSegment = function (mediaSegment) {
        var ms = mediaSegment;
        this._pendingSegments[ms.type].push(ms);
        if (this._config.autoCleanupSourceBuffer && this._needCleanupSourceBuffer()) {
            this._doCleanupSourceBuffer();
        }
        var sb = this._sourceBuffers[ms.type];
        if (sb && !sb.updating && !this._hasPendingRemoveRanges()) {
            this._doAppendSegments();
        }
    };
    MSEController.prototype.flush = function () {
        // remove all appended buffers
        for (var type in this._sourceBuffers) {
            if (!this._sourceBuffers[type]) {
                continue;
            }
            // abort current buffer append algorithm
            var sb = this._sourceBuffers[type];
            if (this._mediaSource.readyState === 'open') {
                try {
                    // If range removal algorithm is running, InvalidStateError will be throwed
                    // Ignore it.
                    sb.abort();
                }
                catch (error) {
                    _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].e(this.TAG, error.message);
                }
            }
            // pending segments should be discard
            var ps = this._pendingSegments[type];
            ps.splice(0, ps.length);
            if (this._mediaSource.readyState === 'closed') {
                // Parent MediaSource object has been detached from HTMLMediaElement
                continue;
            }
            // record ranges to be remove from SourceBuffer
            for (var i = 0; i < sb.buffered.length; i++) {
                var start = sb.buffered.start(i);
                var end = sb.buffered.end(i);
                this._pendingRemoveRanges[type].push({ start: start, end: end });
            }
            // if sb is not updating, let's remove ranges now!
            if (!sb.updating) {
                this._doRemoveRanges();
            }
            // Safari 10 may get InvalidStateError in the later appendBuffer() after SourceBuffer.remove() call
            // Internal parser's state may be invalid at this time. Re-append last InitSegment to workaround.
            // Related issue: https://bugs.webkit.org/show_bug.cgi?id=159230
            if (_utils_browser_js__WEBPACK_IMPORTED_MODULE_2__["default"].safari) {
                var lastInitSegment = this._lastInitSegments[type];
                if (lastInitSegment) {
                    this._pendingSegments[type].push(lastInitSegment);
                    if (!sb.updating) {
                        this._doAppendSegments();
                    }
                }
            }
        }
    };
    MSEController.prototype.endOfStream = function () {
        var ms = this._mediaSource;
        var sb = this._sourceBuffers;
        if (!ms || ms.readyState !== 'open') {
            if (ms && ms.readyState === 'closed' && this._hasPendingSegments()) {
                // If MediaSource hasn't turned into open state, and there're pending segments
                // Mark pending endOfStream, defer call until all pending segments appended complete
                this._hasPendingEos = true;
            }
            return;
        }
        if (sb.video && sb.video.updating || sb.audio && sb.audio.updating) {
            // If any sourcebuffer is updating, defer endOfStream operation
            // See _onSourceBufferUpdateEnd()
            this._hasPendingEos = true;
        }
        else {
            this._hasPendingEos = false;
            // Notify media data loading complete
            // This is helpful for correcting total duration to match last media segment
            // Otherwise MediaElement's ended event may not be triggered
            ms.endOfStream();
        }
    };
    MSEController.prototype._needCleanupSourceBuffer = function () {
        if (!this._config.autoCleanupSourceBuffer) {
            return false;
        }
        var currentTime = this._mediaElementProxy.getCurrentTime();
        for (var type in this._sourceBuffers) {
            var sb = this._sourceBuffers[type];
            if (sb) {
                var buffered = sb.buffered;
                if (buffered.length >= 1) {
                    if (currentTime - buffered.start(0) >= this._config.autoCleanupMaxBackwardDuration) {
                        return true;
                    }
                }
            }
        }
        return false;
    };
    MSEController.prototype._doCleanupSourceBuffer = function () {
        var currentTime = this._mediaElementProxy.getCurrentTime();
        for (var type in this._sourceBuffers) {
            var sb = this._sourceBuffers[type];
            if (sb) {
                var buffered = sb.buffered;
                var doRemove = false;
                for (var i = 0; i < buffered.length; i++) {
                    var start = buffered.start(i);
                    var end = buffered.end(i);
                    if (start <= currentTime && currentTime < end + 3) { // padding 3 seconds
                        if (currentTime - start >= this._config.autoCleanupMaxBackwardDuration) {
                            doRemove = true;
                            var removeEnd = currentTime - this._config.autoCleanupMinBackwardDuration;
                            this._pendingRemoveRanges[type].push({ start: start, end: removeEnd });
                        }
                    }
                    else if (end < currentTime) {
                        doRemove = true;
                        this._pendingRemoveRanges[type].push({ start: start, end: end });
                    }
                }
                if (doRemove && !sb.updating) {
                    this._doRemoveRanges();
                }
            }
        }
    };
    MSEController.prototype._updateMediaSourceDuration = function () {
        var sb = this._sourceBuffers;
        if (this._mediaElementProxy.getReadyState() === 0 || this._mediaSource.readyState !== 'open') {
            return;
        }
        if ((sb.video && sb.video.updating) || (sb.audio && sb.audio.updating)) {
            return;
        }
        var current = this._mediaSource.duration;
        var target = this._pendingMediaDuration;
        if (target > 0 && (isNaN(current) || target > current)) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].v(this.TAG, "Update MediaSource duration from ".concat(current, " to ").concat(target));
            this._mediaSource.duration = target;
        }
        this._requireSetMediaDuration = false;
        this._pendingMediaDuration = 0;
    };
    MSEController.prototype._doRemoveRanges = function () {
        for (var type in this._pendingRemoveRanges) {
            if (!this._sourceBuffers[type] || this._sourceBuffers[type].updating) {
                continue;
            }
            var sb = this._sourceBuffers[type];
            var ranges = this._pendingRemoveRanges[type];
            while (ranges.length && !sb.updating) {
                var range = ranges.shift();
                sb.remove(range.start, range.end);
            }
        }
    };
    MSEController.prototype._doAppendSegments = function () {
        var pendingSegments = this._pendingSegments;
        for (var type in pendingSegments) {
            if (!this._sourceBuffers[type] || this._sourceBuffers[type].updating || this._mediaSource.streaming === false) {
                continue;
            }
            if (pendingSegments[type].length > 0) {
                var segment = pendingSegments[type].shift();
                if (typeof segment.timestampOffset === 'number' && isFinite(segment.timestampOffset)) {
                    // For MPEG audio stream in MSE, if unbuffered-seeking occurred
                    // We need explicitly set timestampOffset to the desired point in timeline for mpeg SourceBuffer.
                    var currentOffset = this._sourceBuffers[type].timestampOffset;
                    var targetOffset = segment.timestampOffset / 1000; // in seconds
                    var delta = Math.abs(currentOffset - targetOffset);
                    if (delta > 0.1) { // If time delta > 100ms
                        _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].v(this.TAG, "Update MPEG audio timestampOffset from ".concat(currentOffset, " to ").concat(targetOffset));
                        this._sourceBuffers[type].timestampOffset = targetOffset;
                    }
                    delete segment.timestampOffset;
                }
                if (!segment.data || segment.data.byteLength === 0) {
                    // Ignore empty buffer
                    continue;
                }
                try {
                    this._sourceBuffers[type].appendBuffer(segment.data);
                    this._isBufferFull = false;
                }
                catch (error) {
                    this._pendingSegments[type].unshift(segment);
                    if (error.code === 22) { // QuotaExceededError
                        /* Notice that FireFox may not throw QuotaExceededError if SourceBuffer is full
                         * Currently we can only do lazy-load to avoid SourceBuffer become scattered.
                         * SourceBuffer eviction policy may be changed in future version of FireFox.
                         *
                         * Related issues:
                         * https://bugzilla.mozilla.org/show_bug.cgi?id=1279885
                         * https://bugzilla.mozilla.org/show_bug.cgi?id=1280023
                         */
                        // report buffer full, abort network IO
                        if (!this._isBufferFull) {
                            this._emitter.emit(_mse_events__WEBPACK_IMPORTED_MODULE_3__["default"].BUFFER_FULL);
                        }
                        this._isBufferFull = true;
                    }
                    else {
                        _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].e(this.TAG, error.message);
                        this._emitter.emit(_mse_events__WEBPACK_IMPORTED_MODULE_3__["default"].ERROR, { code: error.code, msg: error.message });
                    }
                }
            }
        }
    };
    MSEController.prototype._onSourceOpen = function () {
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].v(this.TAG, 'MediaSource onSourceOpen');
        this._mediaSource.removeEventListener('sourceopen', this.e.onSourceOpen);
        // deferred sourcebuffer creation / initialization
        if (this._pendingSourceBufferInit.length > 0) {
            var pendings = this._pendingSourceBufferInit;
            while (pendings.length) {
                var segment = pendings.shift();
                this.appendInitSegment(segment, true);
            }
        }
        // there may be some pending media segments, append them
        if (this._hasPendingSegments()) {
            this._doAppendSegments();
        }
        this._emitter.emit(_mse_events__WEBPACK_IMPORTED_MODULE_3__["default"].SOURCE_OPEN);
    };
    MSEController.prototype._onStartStreaming = function () {
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].v(this.TAG, 'ManagedMediaSource onStartStreaming');
        this._emitter.emit(_mse_events__WEBPACK_IMPORTED_MODULE_3__["default"].START_STREAMING);
    };
    MSEController.prototype._onEndStreaming = function () {
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].v(this.TAG, 'ManagedMediaSource onEndStreaming');
        this._emitter.emit(_mse_events__WEBPACK_IMPORTED_MODULE_3__["default"].END_STREAMING);
    };
    MSEController.prototype._onQualityChange = function () {
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].v(this.TAG, 'ManagedMediaSource onQualityChange');
    };
    MSEController.prototype._onSourceEnded = function () {
        // fired on endOfStream
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].v(this.TAG, 'MediaSource onSourceEnded');
    };
    MSEController.prototype._onSourceClose = function () {
        // fired on detaching from media element
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].v(this.TAG, 'MediaSource onSourceClose');
        if (this._mediaSource && this.e != null) {
            this._mediaSource.removeEventListener('sourceopen', this.e.onSourceOpen);
            this._mediaSource.removeEventListener('sourceended', this.e.onSourceEnded);
            this._mediaSource.removeEventListener('sourceclose', this.e.onSourceClose);
            if (this._useManagedMediaSource) {
                this._mediaSource.removeEventListener('startstreaming', this.e.onStartStreaming);
                this._mediaSource.removeEventListener('endstreaming', this.e.onEndStreaming);
                this._mediaSource.removeEventListener('qualitychange', this.e.onQualityChange);
            }
        }
    };
    MSEController.prototype._hasPendingSegments = function () {
        var ps = this._pendingSegments;
        return ps.video.length > 0 || ps.audio.length > 0;
    };
    MSEController.prototype._hasPendingRemoveRanges = function () {
        var prr = this._pendingRemoveRanges;
        return prr.video.length > 0 || prr.audio.length > 0;
    };
    MSEController.prototype._onSourceBufferUpdateEnd = function () {
        if (this._requireSetMediaDuration) {
            this._updateMediaSourceDuration();
        }
        else if (this._hasPendingRemoveRanges()) {
            this._doRemoveRanges();
        }
        else if (this._hasPendingSegments()) {
            this._doAppendSegments();
        }
        else if (this._hasPendingEos) {
            this.endOfStream();
        }
        this._emitter.emit(_mse_events__WEBPACK_IMPORTED_MODULE_3__["default"].UPDATE_END);
    };
    MSEController.prototype._onSourceBufferError = function (e) {
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].e(this.TAG, "SourceBuffer Error: ".concat(e));
        // this error might not always be fatal, just ignore it
    };
    return MSEController;
}());
/* harmony default export */ __webpack_exports__["default"] = (MSEController);


/***/ }),

/***/ "./src/core/mse-events.ts":
/*!********************************!*\
  !*** ./src/core/mse-events.ts ***!
  \********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var MSEEvents;
(function (MSEEvents) {
    MSEEvents["ERROR"] = "error";
    MSEEvents["SOURCE_OPEN"] = "source_open";
    MSEEvents["UPDATE_END"] = "update_end";
    MSEEvents["BUFFER_FULL"] = "buffer_full";
    MSEEvents["START_STREAMING"] = "start_streaming";
    MSEEvents["END_STREAMING"] = "end_streaming";
})(MSEEvents || (MSEEvents = {}));
;
/* harmony default export */ __webpack_exports__["default"] = (MSEEvents);


/***/ }),

/***/ "./src/core/transmuxer.js":
/*!********************************!*\
  !*** ./src/core/transmuxer.js ***!
  \********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! events */ "./node_modules/events/events.js");
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0___default = /*#__PURE__*/__webpack_require__.n(events__WEBPACK_IMPORTED_MODULE_0__);
/* harmony import */ var _utils_webworkify_webpack__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ../utils/webworkify-webpack */ "./src/utils/webworkify-webpack.js");
/* harmony import */ var _utils_webworkify_webpack__WEBPACK_IMPORTED_MODULE_1___default = /*#__PURE__*/__webpack_require__.n(_utils_webworkify_webpack__WEBPACK_IMPORTED_MODULE_1__);
/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/* harmony import */ var _utils_logging_control_js__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ../utils/logging-control.js */ "./src/utils/logging-control.js");
/* harmony import */ var _transmuxing_controller_js__WEBPACK_IMPORTED_MODULE_4__ = __webpack_require__(/*! ./transmuxing-controller.js */ "./src/core/transmuxing-controller.js");
/* harmony import */ var _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__ = __webpack_require__(/*! ./transmuxing-events */ "./src/core/transmuxing-events.ts");
/* harmony import */ var _transmuxing_worker_js__WEBPACK_IMPORTED_MODULE_6__ = __webpack_require__(/*! ./transmuxing-worker.js */ "./src/core/transmuxing-worker.js");
/* harmony import */ var _media_info_js__WEBPACK_IMPORTED_MODULE_7__ = __webpack_require__(/*! ./media-info.js */ "./src/core/media-info.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */








var Transmuxer = /** @class */ (function () {
    function Transmuxer(mediaDataSource, config) {
        this.TAG = 'Transmuxer';
        this._emitter = new events__WEBPACK_IMPORTED_MODULE_0___default.a();
        if (config.enableWorker && typeof (Worker) !== 'undefined') {
            try {
                this._worker = _utils_webworkify_webpack__WEBPACK_IMPORTED_MODULE_1___default()(/*require.resolve*/(/*! ./transmuxing-worker */ "./src/core/transmuxing-worker.js"));
                this._workerDestroying = false;
                this._worker.addEventListener('message', this._onWorkerMessage.bind(this));
                this._worker.postMessage({ cmd: 'init', param: [mediaDataSource, config] });
                this.e = {
                    onLoggingConfigChanged: this._onLoggingConfigChanged.bind(this)
                };
                _utils_logging_control_js__WEBPACK_IMPORTED_MODULE_3__["default"].registerListener(this.e.onLoggingConfigChanged);
                this._worker.postMessage({ cmd: 'logging_config', param: _utils_logging_control_js__WEBPACK_IMPORTED_MODULE_3__["default"].getConfig() });
            }
            catch (error) {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_2__["default"].e(this.TAG, 'Error while initialize transmuxing worker, fallback to inline transmuxing');
                this._worker = null;
                this._controller = new _transmuxing_controller_js__WEBPACK_IMPORTED_MODULE_4__["default"](mediaDataSource, config);
            }
        }
        else {
            this._controller = new _transmuxing_controller_js__WEBPACK_IMPORTED_MODULE_4__["default"](mediaDataSource, config);
        }
        if (this._controller) {
            var ctl = this._controller;
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].IO_ERROR, this._onIOError.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].DEMUX_ERROR, this._onDemuxError.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].INIT_SEGMENT, this._onInitSegment.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].MEDIA_SEGMENT, this._onMediaSegment.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].LOADING_COMPLETE, this._onLoadingComplete.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].RECOVERED_EARLY_EOF, this._onRecoveredEarlyEof.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].MEDIA_INFO, this._onMediaInfo.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].METADATA_ARRIVED, this._onMetaDataArrived.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].SCRIPTDATA_ARRIVED, this._onScriptDataArrived.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].TIMED_ID3_METADATA_ARRIVED, this._onTimedID3MetadataArrived.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].SYNCHRONOUS_KLV_METADATA_ARRIVED, this._onSynchronousKLVMetadataArrived.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].ASYNCHRONOUS_KLV_METADATA_ARRIVED, this._onAsynchronousKLVMetadataArrived.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].SMPTE2038_METADATA_ARRIVED, this._onSMPTE2038MetadataArrived.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].SCTE35_METADATA_ARRIVED, this._onSCTE35MetadataArrived.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].PES_PRIVATE_DATA_DESCRIPTOR, this._onPESPrivateDataDescriptor.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].PES_PRIVATE_DATA_ARRIVED, this._onPESPrivateDataArrived.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].STATISTICS_INFO, this._onStatisticsInfo.bind(this));
            ctl.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].RECOMMEND_SEEKPOINT, this._onRecommendSeekpoint.bind(this));
        }
    }
    Transmuxer.prototype.destroy = function () {
        if (this._worker) {
            if (!this._workerDestroying) {
                this._workerDestroying = true;
                this._worker.postMessage({ cmd: 'destroy' });
                _utils_logging_control_js__WEBPACK_IMPORTED_MODULE_3__["default"].removeListener(this.e.onLoggingConfigChanged);
                this.e = null;
            }
        }
        else {
            this._controller.destroy();
            this._controller = null;
        }
        this._emitter.removeAllListeners();
        this._emitter = null;
    };
    Transmuxer.prototype.on = function (event, listener) {
        this._emitter.addListener(event, listener);
    };
    Transmuxer.prototype.off = function (event, listener) {
        this._emitter.removeListener(event, listener);
    };
    Transmuxer.prototype.hasWorker = function () {
        return this._worker != null;
    };
    Transmuxer.prototype.open = function () {
        if (this._worker) {
            this._worker.postMessage({ cmd: 'start' });
        }
        else {
            this._controller.start();
        }
    };
    Transmuxer.prototype.close = function () {
        if (this._worker) {
            this._worker.postMessage({ cmd: 'stop' });
        }
        else {
            this._controller.stop();
        }
    };
    Transmuxer.prototype.seek = function (milliseconds) {
        if (this._worker) {
            this._worker.postMessage({ cmd: 'seek', param: milliseconds });
        }
        else {
            this._controller.seek(milliseconds);
        }
    };
    Transmuxer.prototype.pause = function () {
        if (this._worker) {
            this._worker.postMessage({ cmd: 'pause' });
        }
        else {
            this._controller.pause();
        }
    };
    Transmuxer.prototype.resume = function () {
        if (this._worker) {
            this._worker.postMessage({ cmd: 'resume' });
        }
        else {
            this._controller.resume();
        }
    };
    Transmuxer.prototype._onInitSegment = function (type, initSegment) {
        var _this = this;
        // do async invoke
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].INIT_SEGMENT, type, initSegment);
        });
    };
    Transmuxer.prototype._onMediaSegment = function (type, mediaSegment) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].MEDIA_SEGMENT, type, mediaSegment);
        });
    };
    Transmuxer.prototype._onLoadingComplete = function () {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].LOADING_COMPLETE);
        });
    };
    Transmuxer.prototype._onRecoveredEarlyEof = function () {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].RECOVERED_EARLY_EOF);
        });
    };
    Transmuxer.prototype._onMediaInfo = function (mediaInfo) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].MEDIA_INFO, mediaInfo);
        });
    };
    Transmuxer.prototype._onMetaDataArrived = function (metadata) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].METADATA_ARRIVED, metadata);
        });
    };
    Transmuxer.prototype._onScriptDataArrived = function (data) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].SCRIPTDATA_ARRIVED, data);
        });
    };
    Transmuxer.prototype._onTimedID3MetadataArrived = function (data) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].TIMED_ID3_METADATA_ARRIVED, data);
        });
    };
    Transmuxer.prototype._onPGSSubtitleArrived = function (data) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].PGS_SUBTITLE_ARRIVED, data);
        });
    };
    Transmuxer.prototype._onSynchronousKLVMetadataArrived = function (data) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].SYNCHRONOUS_KLV_METADATA_ARRIVED, data);
        });
    };
    Transmuxer.prototype._onAsynchronousKLVMetadataArrived = function (data) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].ASYNCHRONOUS_KLV_METADATA_ARRIVED, data);
        });
    };
    Transmuxer.prototype._onSMPTE2038MetadataArrived = function (data) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].SMPTE2038_METADATA_ARRIVED, data);
        });
    };
    Transmuxer.prototype._onSCTE35MetadataArrived = function (data) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].SCTE35_METADATA_ARRIVED, data);
        });
    };
    Transmuxer.prototype._onPESPrivateDataDescriptor = function (data) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].PES_PRIVATE_DATA_DESCRIPTOR, data);
        });
    };
    Transmuxer.prototype._onPESPrivateDataArrived = function (data) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].PES_PRIVATE_DATA_ARRIVED, data);
        });
    };
    Transmuxer.prototype._onStatisticsInfo = function (statisticsInfo) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].STATISTICS_INFO, statisticsInfo);
        });
    };
    Transmuxer.prototype._onIOError = function (type, info) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].IO_ERROR, type, info);
        });
    };
    Transmuxer.prototype._onDemuxError = function (type, info) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].DEMUX_ERROR, type, info);
        });
    };
    Transmuxer.prototype._onRecommendSeekpoint = function (milliseconds) {
        var _this = this;
        Promise.resolve().then(function () {
            _this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].RECOMMEND_SEEKPOINT, milliseconds);
        });
    };
    Transmuxer.prototype._onLoggingConfigChanged = function (config) {
        if (this._worker) {
            this._worker.postMessage({ cmd: 'logging_config', param: config });
        }
    };
    Transmuxer.prototype._onWorkerMessage = function (e) {
        var message = e.data;
        var data = message.data;
        if (message.msg === 'destroyed' || this._workerDestroying) {
            this._workerDestroying = false;
            this._worker.terminate();
            this._worker = null;
            return;
        }
        switch (message.msg) {
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].INIT_SEGMENT:
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].MEDIA_SEGMENT:
                this._emitter.emit(message.msg, data.type, data.data);
                break;
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].LOADING_COMPLETE:
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].RECOVERED_EARLY_EOF:
                this._emitter.emit(message.msg);
                break;
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].MEDIA_INFO:
                Object.setPrototypeOf(data, _media_info_js__WEBPACK_IMPORTED_MODULE_7__["default"].prototype);
                this._emitter.emit(message.msg, data);
                break;
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].METADATA_ARRIVED:
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].SCRIPTDATA_ARRIVED:
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].TIMED_ID3_METADATA_ARRIVED:
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].PGS_SUBTITLE_ARRIVED:
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].SYNCHRONOUS_KLV_METADATA_ARRIVED:
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].ASYNCHRONOUS_KLV_METADATA_ARRIVED:
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].SMPTE2038_METADATA_ARRIVED:
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].SCTE35_METADATA_ARRIVED:
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].PES_PRIVATE_DATA_DESCRIPTOR:
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].PES_PRIVATE_DATA_ARRIVED:
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].STATISTICS_INFO:
                this._emitter.emit(message.msg, data);
                break;
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].IO_ERROR:
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].DEMUX_ERROR:
                this._emitter.emit(message.msg, data.type, data.info);
                break;
            case _transmuxing_events__WEBPACK_IMPORTED_MODULE_5__["default"].RECOMMEND_SEEKPOINT:
                this._emitter.emit(message.msg, data);
                break;
            case 'logcat_callback':
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_2__["default"].emitter.emit('log', data.type, data.logcat);
                break;
            default:
                break;
        }
    };
    return Transmuxer;
}());
/* harmony default export */ __webpack_exports__["default"] = (Transmuxer);


/***/ }),

/***/ "./src/core/transmuxing-controller.js":
/*!********************************************!*\
  !*** ./src/core/transmuxing-controller.js ***!
  \********************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! events */ "./node_modules/events/events.js");
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0___default = /*#__PURE__*/__webpack_require__.n(events__WEBPACK_IMPORTED_MODULE_0__);
/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/* harmony import */ var _utils_browser_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ../utils/browser.js */ "./src/utils/browser.js");
/* harmony import */ var _media_info_js__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ./media-info.js */ "./src/core/media-info.js");
/* harmony import */ var _demux_flv_demuxer_js__WEBPACK_IMPORTED_MODULE_4__ = __webpack_require__(/*! ../demux/flv-demuxer.js */ "./src/demux/flv-demuxer.js");
/* harmony import */ var _demux_ts_demuxer__WEBPACK_IMPORTED_MODULE_5__ = __webpack_require__(/*! ../demux/ts-demuxer */ "./src/demux/ts-demuxer.ts");
/* harmony import */ var _remux_mp4_remuxer_js__WEBPACK_IMPORTED_MODULE_6__ = __webpack_require__(/*! ../remux/mp4-remuxer.js */ "./src/remux/mp4-remuxer.js");
/* harmony import */ var _demux_demux_errors_js__WEBPACK_IMPORTED_MODULE_7__ = __webpack_require__(/*! ../demux/demux-errors.js */ "./src/demux/demux-errors.js");
/* harmony import */ var _io_io_controller_js__WEBPACK_IMPORTED_MODULE_8__ = __webpack_require__(/*! ../io/io-controller.js */ "./src/io/io-controller.js");
/* harmony import */ var _transmuxing_events__WEBPACK_IMPORTED_MODULE_9__ = __webpack_require__(/*! ./transmuxing-events */ "./src/core/transmuxing-events.ts");
/* harmony import */ var _io_loader_js__WEBPACK_IMPORTED_MODULE_10__ = __webpack_require__(/*! ../io/loader.js */ "./src/io/loader.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */











// Transmuxing (IO, Demuxing, Remuxing) controller, with multipart support
var TransmuxingController = /** @class */ (function () {
    function TransmuxingController(mediaDataSource, config) {
        this.TAG = 'TransmuxingController';
        this._emitter = new events__WEBPACK_IMPORTED_MODULE_0___default.a();
        this._config = config;
        // treat single part media as multipart media, which has only one segment
        if (!mediaDataSource.segments) {
            mediaDataSource.segments = [{
                    duration: mediaDataSource.duration,
                    filesize: mediaDataSource.filesize,
                    url: mediaDataSource.url
                }];
        }
        // fill in default IO params if not exists
        if (typeof mediaDataSource.cors !== 'boolean') {
            mediaDataSource.cors = true;
        }
        if (typeof mediaDataSource.withCredentials !== 'boolean') {
            mediaDataSource.withCredentials = false;
        }
        this._mediaDataSource = mediaDataSource;
        this._currentSegmentIndex = 0;
        var totalDuration = 0;
        this._mediaDataSource.segments.forEach(function (segment) {
            // timestampBase for each segment, and calculate total duration
            segment.timestampBase = totalDuration;
            totalDuration += segment.duration;
            // params needed by IOController
            segment.cors = mediaDataSource.cors;
            segment.withCredentials = mediaDataSource.withCredentials;
            // referrer policy control, if exist
            if (config.referrerPolicy) {
                segment.referrerPolicy = config.referrerPolicy;
            }
        });
        if (!isNaN(totalDuration) && this._mediaDataSource.duration !== totalDuration) {
            this._mediaDataSource.duration = totalDuration;
        }
        this._mediaInfo = null;
        this._demuxer = null;
        this._remuxer = null;
        this._ioctl = null;
        this._pendingSeekTime = null;
        this._pendingResolveSeekPoint = null;
        this._statisticsReporter = null;
    }
    TransmuxingController.prototype.destroy = function () {
        this._mediaInfo = null;
        this._mediaDataSource = null;
        if (this._statisticsReporter) {
            this._disableStatisticsReporter();
        }
        if (this._ioctl) {
            this._ioctl.destroy();
            this._ioctl = null;
        }
        if (this._demuxer) {
            this._demuxer.destroy();
            this._demuxer = null;
        }
        if (this._remuxer) {
            this._remuxer.destroy();
            this._remuxer = null;
        }
        this._emitter.removeAllListeners();
        this._emitter = null;
    };
    TransmuxingController.prototype.on = function (event, listener) {
        this._emitter.addListener(event, listener);
    };
    TransmuxingController.prototype.off = function (event, listener) {
        this._emitter.removeListener(event, listener);
    };
    TransmuxingController.prototype.start = function () {
        this._loadSegment(0);
        this._enableStatisticsReporter();
    };
    TransmuxingController.prototype._loadSegment = function (segmentIndex, optionalFrom) {
        this._currentSegmentIndex = segmentIndex;
        var dataSource = this._mediaDataSource.segments[segmentIndex];
        var ioctl = this._ioctl = new _io_io_controller_js__WEBPACK_IMPORTED_MODULE_8__["default"](dataSource, this._config, segmentIndex);
        ioctl.onError = this._onIOException.bind(this);
        ioctl.onSeeked = this._onIOSeeked.bind(this);
        ioctl.onComplete = this._onIOComplete.bind(this);
        ioctl.onRedirect = this._onIORedirect.bind(this);
        ioctl.onRecoveredEarlyEof = this._onIORecoveredEarlyEof.bind(this);
        if (optionalFrom) {
            this._demuxer.bindDataSource(this._ioctl);
        }
        else {
            ioctl.onDataArrival = this._onInitChunkArrival.bind(this);
        }
        ioctl.open(optionalFrom);
    };
    TransmuxingController.prototype.stop = function () {
        this._internalAbort();
        this._disableStatisticsReporter();
    };
    TransmuxingController.prototype._internalAbort = function () {
        if (this._ioctl) {
            this._ioctl.destroy();
            this._ioctl = null;
        }
    };
    TransmuxingController.prototype.pause = function () {
        if (this._ioctl && this._ioctl.isWorking()) {
            this._ioctl.pause();
            this._disableStatisticsReporter();
        }
    };
    TransmuxingController.prototype.resume = function () {
        if (this._ioctl && this._ioctl.isPaused()) {
            this._ioctl.resume();
            this._enableStatisticsReporter();
        }
    };
    TransmuxingController.prototype.seek = function (milliseconds) {
        if (this._mediaInfo == null || !this._mediaInfo.isSeekable()) {
            return;
        }
        var targetSegmentIndex = this._searchSegmentIndexContains(milliseconds);
        if (targetSegmentIndex === this._currentSegmentIndex) {
            // intra-segment seeking
            var segmentInfo = this._mediaInfo.segments[targetSegmentIndex];
            if (segmentInfo == undefined) {
                // current segment loading started, but mediainfo hasn't received yet
                // wait for the metadata loaded, then seek to expected position
                this._pendingSeekTime = milliseconds;
            }
            else {
                var keyframe = segmentInfo.getNearestKeyframe(milliseconds);
                this._remuxer.seek(keyframe.milliseconds);
                this._ioctl.seek(keyframe.fileposition);
                // Will be resolved in _onRemuxerMediaSegmentArrival()
                this._pendingResolveSeekPoint = keyframe.milliseconds;
            }
        }
        else {
            // cross-segment seeking
            var targetSegmentInfo = this._mediaInfo.segments[targetSegmentIndex];
            if (targetSegmentInfo == undefined) {
                // target segment hasn't been loaded. We need metadata then seek to expected time
                this._pendingSeekTime = milliseconds;
                this._internalAbort();
                this._remuxer.seek();
                this._remuxer.insertDiscontinuity();
                this._loadSegment(targetSegmentIndex);
                // Here we wait for the metadata loaded, then seek to expected position
            }
            else {
                // We have target segment's metadata, direct seek to target position
                var keyframe = targetSegmentInfo.getNearestKeyframe(milliseconds);
                this._internalAbort();
                this._remuxer.seek(milliseconds);
                this._remuxer.insertDiscontinuity();
                this._demuxer.resetMediaInfo();
                this._demuxer.timestampBase = this._mediaDataSource.segments[targetSegmentIndex].timestampBase;
                this._loadSegment(targetSegmentIndex, keyframe.fileposition);
                this._pendingResolveSeekPoint = keyframe.milliseconds;
                this._reportSegmentMediaInfo(targetSegmentIndex);
            }
        }
        this._enableStatisticsReporter();
    };
    TransmuxingController.prototype._searchSegmentIndexContains = function (milliseconds) {
        var segments = this._mediaDataSource.segments;
        var idx = segments.length - 1;
        for (var i = 0; i < segments.length; i++) {
            if (milliseconds < segments[i].timestampBase) {
                idx = i - 1;
                break;
            }
        }
        return idx;
    };
    TransmuxingController.prototype._onInitChunkArrival = function (data, byteStart) {
        var _this = this;
        var consumed = 0;
        if (byteStart > 0) {
            // IOController seeked immediately after opened, byteStart > 0 callback may received
            this._demuxer.bindDataSource(this._ioctl);
            this._demuxer.timestampBase = this._mediaDataSource.segments[this._currentSegmentIndex].timestampBase;
            consumed = this._demuxer.parseChunks(data, byteStart);
        }
        else {
            // byteStart == 0, Initial data, probe it first
            var probeData = null;
            // Try probing input data as FLV first
            probeData = _demux_flv_demuxer_js__WEBPACK_IMPORTED_MODULE_4__["default"].probe(data);
            if (probeData.match) {
                // Hit as FLV
                this._setupFLVDemuxerRemuxer(probeData);
                consumed = this._demuxer.parseChunks(data, byteStart);
            }
            if (!probeData.match && !probeData.needMoreData) {
                // Non-FLV, try MPEG-TS probe
                probeData = _demux_ts_demuxer__WEBPACK_IMPORTED_MODULE_5__["default"].probe(data);
                if (probeData.match) {
                    // Hit as MPEG-TS
                    this._setupTSDemuxerRemuxer(probeData);
                    consumed = this._demuxer.parseChunks(data, byteStart);
                }
            }
            if (!probeData.match && !probeData.needMoreData) {
                // Both probing as FLV / MPEG-TS failed, report error
                probeData = null;
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].e(this.TAG, 'Non MPEG-TS/FLV, Unsupported media type!');
                Promise.resolve().then(function () {
                    _this._internalAbort();
                });
                this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].DEMUX_ERROR, _demux_demux_errors_js__WEBPACK_IMPORTED_MODULE_7__["default"].FORMAT_UNSUPPORTED, 'Non MPEG-TS/FLV, Unsupported media type!');
                // Leave consumed as 0
            }
        }
        return consumed;
    };
    TransmuxingController.prototype._setupFLVDemuxerRemuxer = function (probeData) {
        this._demuxer = new _demux_flv_demuxer_js__WEBPACK_IMPORTED_MODULE_4__["default"](probeData, this._config);
        if (!this._remuxer) {
            this._remuxer = new _remux_mp4_remuxer_js__WEBPACK_IMPORTED_MODULE_6__["default"](this._config);
        }
        var mds = this._mediaDataSource;
        if (mds.duration != undefined && !isNaN(mds.duration)) {
            this._demuxer.overridedDuration = mds.duration;
        }
        if (typeof mds.hasAudio === 'boolean') {
            this._demuxer.overridedHasAudio = mds.hasAudio;
        }
        if (typeof mds.hasVideo === 'boolean') {
            this._demuxer.overridedHasVideo = mds.hasVideo;
        }
        this._demuxer.timestampBase = mds.segments[this._currentSegmentIndex].timestampBase;
        this._demuxer.onError = this._onDemuxException.bind(this);
        this._demuxer.onMediaInfo = this._onMediaInfo.bind(this);
        this._demuxer.onMetaDataArrived = this._onMetaDataArrived.bind(this);
        this._demuxer.onScriptDataArrived = this._onScriptDataArrived.bind(this);
        this._remuxer.bindDataSource(this._demuxer
            .bindDataSource(this._ioctl));
        this._remuxer.onInitSegment = this._onRemuxerInitSegmentArrival.bind(this);
        this._remuxer.onMediaSegment = this._onRemuxerMediaSegmentArrival.bind(this);
    };
    TransmuxingController.prototype._setupTSDemuxerRemuxer = function (probeData) {
        var demuxer = this._demuxer = new _demux_ts_demuxer__WEBPACK_IMPORTED_MODULE_5__["default"](probeData, this._config);
        if (!this._remuxer) {
            this._remuxer = new _remux_mp4_remuxer_js__WEBPACK_IMPORTED_MODULE_6__["default"](this._config);
        }
        demuxer.onError = this._onDemuxException.bind(this);
        demuxer.onMediaInfo = this._onMediaInfo.bind(this);
        demuxer.onMetaDataArrived = this._onMetaDataArrived.bind(this);
        demuxer.onTimedID3Metadata = this._onTimedID3Metadata.bind(this);
        demuxer.onPGSSubtitleData = this._onPGSSubtitle.bind(this);
        demuxer.onSynchronousKLVMetadata = this._onSynchronousKLVMetadata.bind(this);
        demuxer.onAsynchronousKLVMetadata = this._onAsynchronousKLVMetadata.bind(this);
        demuxer.onSMPTE2038Metadata = this._onSMPTE2038Metadata.bind(this);
        demuxer.onSCTE35Metadata = this._onSCTE35Metadata.bind(this);
        demuxer.onPESPrivateDataDescriptor = this._onPESPrivateDataDescriptor.bind(this);
        demuxer.onPESPrivateData = this._onPESPrivateData.bind(this);
        this._remuxer.bindDataSource(this._demuxer);
        this._demuxer.bindDataSource(this._ioctl);
        this._remuxer.onInitSegment = this._onRemuxerInitSegmentArrival.bind(this);
        this._remuxer.onMediaSegment = this._onRemuxerMediaSegmentArrival.bind(this);
    };
    TransmuxingController.prototype._onMediaInfo = function (mediaInfo) {
        var _this = this;
        if (this._mediaInfo == null) {
            // Store first segment's mediainfo as global mediaInfo
            this._mediaInfo = Object.assign({}, mediaInfo);
            this._mediaInfo.keyframesIndex = null;
            this._mediaInfo.segments = [];
            this._mediaInfo.segmentCount = this._mediaDataSource.segments.length;
            Object.setPrototypeOf(this._mediaInfo, _media_info_js__WEBPACK_IMPORTED_MODULE_3__["default"].prototype);
        }
        var segmentInfo = Object.assign({}, mediaInfo);
        Object.setPrototypeOf(segmentInfo, _media_info_js__WEBPACK_IMPORTED_MODULE_3__["default"].prototype);
        this._mediaInfo.segments[this._currentSegmentIndex] = segmentInfo;
        // notify mediaInfo update
        this._reportSegmentMediaInfo(this._currentSegmentIndex);
        if (this._pendingSeekTime != null) {
            Promise.resolve().then(function () {
                var target = _this._pendingSeekTime;
                _this._pendingSeekTime = null;
                _this.seek(target);
            });
        }
    };
    TransmuxingController.prototype._onMetaDataArrived = function (metadata) {
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].METADATA_ARRIVED, metadata);
    };
    TransmuxingController.prototype._onScriptDataArrived = function (data) {
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].SCRIPTDATA_ARRIVED, data);
    };
    TransmuxingController.prototype._onTimedID3Metadata = function (timed_id3_metadata) {
        var timestamp_base = this._remuxer.getTimestampBase();
        if (timestamp_base == undefined) {
            return;
        }
        if (timed_id3_metadata.pts != undefined) {
            timed_id3_metadata.pts -= timestamp_base;
        }
        if (timed_id3_metadata.dts != undefined) {
            timed_id3_metadata.dts -= timestamp_base;
        }
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].TIMED_ID3_METADATA_ARRIVED, timed_id3_metadata);
    };
    TransmuxingController.prototype._onPGSSubtitle = function (pgs_data) {
        var timestamp_base = this._remuxer.getTimestampBase();
        if (timestamp_base == undefined) {
            return;
        }
        if (pgs_data.pts != undefined) {
            pgs_data.pts -= timestamp_base;
        }
        if (pgs_data.dts != undefined) {
            pgs_data.dts -= timestamp_base;
        }
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].PGS_SUBTITLE_ARRIVED, pgs_data);
    };
    TransmuxingController.prototype._onSynchronousKLVMetadata = function (synchronous_klv_metadata) {
        var timestamp_base = this._remuxer.getTimestampBase();
        if (timestamp_base == undefined) {
            return;
        }
        if (synchronous_klv_metadata.pts != undefined) {
            synchronous_klv_metadata.pts -= timestamp_base;
        }
        if (synchronous_klv_metadata.dts != undefined) {
            synchronous_klv_metadata.dts -= timestamp_base;
        }
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].SYNCHRONOUS_KLV_METADATA_ARRIVED, synchronous_klv_metadata);
    };
    TransmuxingController.prototype._onAsynchronousKLVMetadata = function (asynchronous_klv_metadata) {
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].ASYNCHRONOUS_KLV_METADATA_ARRIVED, asynchronous_klv_metadata);
    };
    TransmuxingController.prototype._onSMPTE2038Metadata = function (smpte2038_metadata) {
        var timestamp_base = this._remuxer.getTimestampBase();
        if (timestamp_base == undefined) {
            return;
        }
        if (smpte2038_metadata.pts != undefined) {
            smpte2038_metadata.pts -= timestamp_base;
        }
        if (smpte2038_metadata.dts != undefined) {
            smpte2038_metadata.dts -= timestamp_base;
        }
        if (smpte2038_metadata.nearest_pts != undefined) {
            smpte2038_metadata.nearest_pts -= timestamp_base;
        }
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].SMPTE2038_METADATA_ARRIVED, smpte2038_metadata);
    };
    TransmuxingController.prototype._onSCTE35Metadata = function (scte35) {
        var timestamp_base = this._remuxer.getTimestampBase();
        if (timestamp_base == undefined) {
            return;
        }
        if (scte35.pts != undefined) {
            scte35.pts -= timestamp_base;
        }
        if (scte35.nearest_pts != undefined) {
            scte35.nearest_pts -= timestamp_base;
        }
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].SCTE35_METADATA_ARRIVED, scte35);
    };
    TransmuxingController.prototype._onPESPrivateDataDescriptor = function (descriptor) {
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].PES_PRIVATE_DATA_DESCRIPTOR, descriptor);
    };
    TransmuxingController.prototype._onPESPrivateData = function (private_data) {
        var timestamp_base = this._remuxer.getTimestampBase();
        if (timestamp_base == undefined) {
            return;
        }
        if (private_data.pts != undefined) {
            private_data.pts -= timestamp_base;
        }
        if (private_data.nearest_pts != undefined) {
            private_data.nearest_pts -= timestamp_base;
        }
        if (private_data.dts != undefined) {
            private_data.dts -= timestamp_base;
        }
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].PES_PRIVATE_DATA_ARRIVED, private_data);
    };
    TransmuxingController.prototype._onIOSeeked = function () {
        this._remuxer.insertDiscontinuity();
    };
    TransmuxingController.prototype._onIOComplete = function (extraData) {
        var segmentIndex = extraData;
        var nextSegmentIndex = segmentIndex + 1;
        if (nextSegmentIndex < this._mediaDataSource.segments.length) {
            this._internalAbort();
            if (this._remuxer) {
                this._remuxer.flushStashedSamples();
            }
            this._loadSegment(nextSegmentIndex);
        }
        else {
            if (this._remuxer) {
                this._remuxer.flushStashedSamples();
            }
            this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].LOADING_COMPLETE);
            this._disableStatisticsReporter();
        }
    };
    TransmuxingController.prototype._onIORedirect = function (redirectedURL) {
        var segmentIndex = this._ioctl.extraData;
        this._mediaDataSource.segments[segmentIndex].redirectedURL = redirectedURL;
    };
    TransmuxingController.prototype._onIORecoveredEarlyEof = function () {
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].RECOVERED_EARLY_EOF);
    };
    TransmuxingController.prototype._onIOException = function (type, info) {
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].e(this.TAG, "IOException: type = ".concat(type, ", code = ").concat(info.code, ", msg = ").concat(info.msg));
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].IO_ERROR, type, info);
        this._disableStatisticsReporter();
    };
    TransmuxingController.prototype._onDemuxException = function (type, info) {
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].e(this.TAG, "DemuxException: type = ".concat(type, ", info = ").concat(info));
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].DEMUX_ERROR, type, info);
    };
    TransmuxingController.prototype._onRemuxerInitSegmentArrival = function (type, initSegment) {
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].INIT_SEGMENT, type, initSegment);
    };
    TransmuxingController.prototype._onRemuxerMediaSegmentArrival = function (type, mediaSegment) {
        if (this._pendingSeekTime != null) {
            // Media segments after new-segment cross-seeking should be dropped.
            return;
        }
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].MEDIA_SEGMENT, type, mediaSegment);
        // Resolve pending seekPoint
        if (this._pendingResolveSeekPoint != null && type === 'video') {
            var syncPoints = mediaSegment.info.syncPoints;
            var seekpoint = this._pendingResolveSeekPoint;
            this._pendingResolveSeekPoint = null;
            // Safari: Pass PTS for recommend_seekpoint
            if (_utils_browser_js__WEBPACK_IMPORTED_MODULE_2__["default"].safari && syncPoints.length > 0 && syncPoints[0].originalDts === seekpoint) {
                seekpoint = syncPoints[0].pts;
            }
            // else: use original DTS (keyframe.milliseconds)
            this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].RECOMMEND_SEEKPOINT, seekpoint);
        }
    };
    TransmuxingController.prototype._enableStatisticsReporter = function () {
        if (this._statisticsReporter == null) {
            this._statisticsReporter = self.setInterval(this._reportStatisticsInfo.bind(this), this._config.statisticsInfoReportInterval);
        }
    };
    TransmuxingController.prototype._disableStatisticsReporter = function () {
        if (this._statisticsReporter) {
            self.clearInterval(this._statisticsReporter);
            this._statisticsReporter = null;
        }
    };
    TransmuxingController.prototype._reportSegmentMediaInfo = function (segmentIndex) {
        var segmentInfo = this._mediaInfo.segments[segmentIndex];
        var exportInfo = Object.assign({}, segmentInfo);
        exportInfo.duration = this._mediaInfo.duration;
        exportInfo.segmentCount = this._mediaInfo.segmentCount;
        delete exportInfo.segments;
        delete exportInfo.keyframesIndex;
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].MEDIA_INFO, exportInfo);
    };
    TransmuxingController.prototype._reportStatisticsInfo = function () {
        var info = {};
        info.url = this._ioctl.currentURL;
        info.hasRedirect = this._ioctl.hasRedirect;
        if (info.hasRedirect) {
            info.redirectedURL = this._ioctl.currentRedirectedURL;
        }
        info.speed = this._ioctl.currentSpeed;
        info.loaderType = this._ioctl.loaderType;
        info.currentSegmentIndex = this._currentSegmentIndex;
        info.totalSegmentCount = this._mediaDataSource.segments.length;
        this._emitter.emit(_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].STATISTICS_INFO, info);
    };
    return TransmuxingController;
}());
/* harmony default export */ __webpack_exports__["default"] = (TransmuxingController);


/***/ }),

/***/ "./src/core/transmuxing-events.ts":
/*!****************************************!*\
  !*** ./src/core/transmuxing-events.ts ***!
  \****************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var TransmuxingEvents;
(function (TransmuxingEvents) {
    TransmuxingEvents["IO_ERROR"] = "io_error";
    TransmuxingEvents["DEMUX_ERROR"] = "demux_error";
    TransmuxingEvents["INIT_SEGMENT"] = "init_segment";
    TransmuxingEvents["MEDIA_SEGMENT"] = "media_segment";
    TransmuxingEvents["LOADING_COMPLETE"] = "loading_complete";
    TransmuxingEvents["RECOVERED_EARLY_EOF"] = "recovered_early_eof";
    TransmuxingEvents["MEDIA_INFO"] = "media_info";
    TransmuxingEvents["METADATA_ARRIVED"] = "metadata_arrived";
    TransmuxingEvents["SCRIPTDATA_ARRIVED"] = "scriptdata_arrived";
    TransmuxingEvents["TIMED_ID3_METADATA_ARRIVED"] = "timed_id3_metadata_arrived";
    TransmuxingEvents["PGS_SUBTITLE_ARRIVED"] = "pgs_subtitle_arrived";
    TransmuxingEvents["SYNCHRONOUS_KLV_METADATA_ARRIVED"] = "synchronous_klv_metadata_arrived";
    TransmuxingEvents["ASYNCHRONOUS_KLV_METADATA_ARRIVED"] = "asynchronous_klv_metadata_arrived";
    TransmuxingEvents["SMPTE2038_METADATA_ARRIVED"] = "smpte2038_metadata_arrived";
    TransmuxingEvents["SCTE35_METADATA_ARRIVED"] = "scte35_metadata_arrived";
    TransmuxingEvents["PES_PRIVATE_DATA_DESCRIPTOR"] = "pes_private_data_descriptor";
    TransmuxingEvents["PES_PRIVATE_DATA_ARRIVED"] = "pes_private_data_arrived";
    TransmuxingEvents["STATISTICS_INFO"] = "statistics_info";
    TransmuxingEvents["RECOMMEND_SEEKPOINT"] = "recommend_seekpoint";
})(TransmuxingEvents || (TransmuxingEvents = {}));
;
/* harmony default export */ __webpack_exports__["default"] = (TransmuxingEvents);


/***/ }),

/***/ "./src/core/transmuxing-worker.js":
/*!****************************************!*\
  !*** ./src/core/transmuxing-worker.js ***!
  \****************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/* harmony import */ var _utils_logging_control_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ../utils/logging-control.js */ "./src/utils/logging-control.js");
/* harmony import */ var _utils_polyfill_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ../utils/polyfill.js */ "./src/utils/polyfill.js");
/* harmony import */ var _transmuxing_controller_js__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ./transmuxing-controller.js */ "./src/core/transmuxing-controller.js");
/* harmony import */ var _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__ = __webpack_require__(/*! ./transmuxing-events */ "./src/core/transmuxing-events.ts");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */





/* post message to worker:
   data: {
       cmd: string
       param: any
   }

   receive message from worker:
   data: {
       msg: string,
       data: any
   }
 */
var TransmuxingWorker = function (self) {
    var TAG = 'TransmuxingWorker';
    var controller = null;
    var logcatListener = onLogcatCallback.bind(this);
    _utils_polyfill_js__WEBPACK_IMPORTED_MODULE_2__["default"].install();
    self.addEventListener('message', function (e) {
        switch (e.data.cmd) {
            case 'init':
                controller = new _transmuxing_controller_js__WEBPACK_IMPORTED_MODULE_3__["default"](e.data.param[0], e.data.param[1]);
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].IO_ERROR, onIOError.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].DEMUX_ERROR, onDemuxError.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].INIT_SEGMENT, onInitSegment.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].MEDIA_SEGMENT, onMediaSegment.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].LOADING_COMPLETE, onLoadingComplete.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].RECOVERED_EARLY_EOF, onRecoveredEarlyEof.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].MEDIA_INFO, onMediaInfo.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].METADATA_ARRIVED, onMetaDataArrived.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].SCRIPTDATA_ARRIVED, onScriptDataArrived.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].TIMED_ID3_METADATA_ARRIVED, onTimedID3MetadataArrived.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].PGS_SUBTITLE_ARRIVED, onPGSSubtitleDataArrived.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].SYNCHRONOUS_KLV_METADATA_ARRIVED, onSynchronousKLVMetadataArrived.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].ASYNCHRONOUS_KLV_METADATA_ARRIVED, onAsynchronousKLVMetadataArrived.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].SMPTE2038_METADATA_ARRIVED, onSMPTE2038MetadataArrived.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].SCTE35_METADATA_ARRIVED, onSCTE35MetadataArrived.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].PES_PRIVATE_DATA_DESCRIPTOR, onPESPrivateDataDescriptor.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].PES_PRIVATE_DATA_ARRIVED, onPESPrivateDataArrived.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].STATISTICS_INFO, onStatisticsInfo.bind(this));
                controller.on(_transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].RECOMMEND_SEEKPOINT, onRecommendSeekpoint.bind(this));
                break;
            case 'destroy':
                if (controller) {
                    controller.destroy();
                    controller = null;
                }
                self.postMessage({ msg: 'destroyed' });
                break;
            case 'start':
                controller.start();
                break;
            case 'stop':
                controller.stop();
                break;
            case 'seek':
                controller.seek(e.data.param);
                break;
            case 'pause':
                controller.pause();
                break;
            case 'resume':
                controller.resume();
                break;
            case 'logging_config': {
                var config = e.data.param;
                _utils_logging_control_js__WEBPACK_IMPORTED_MODULE_1__["default"].applyConfig(config);
                if (config.enableCallback === true) {
                    _utils_logging_control_js__WEBPACK_IMPORTED_MODULE_1__["default"].addLogListener(logcatListener);
                }
                else {
                    _utils_logging_control_js__WEBPACK_IMPORTED_MODULE_1__["default"].removeLogListener(logcatListener);
                }
                break;
            }
        }
    });
    function onInitSegment(type, initSegment) {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].INIT_SEGMENT,
            data: {
                type: type,
                data: initSegment
            }
        };
        self.postMessage(obj, [initSegment.data]); // data: ArrayBuffer
    }
    function onMediaSegment(type, mediaSegment) {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].MEDIA_SEGMENT,
            data: {
                type: type,
                data: mediaSegment
            }
        };
        self.postMessage(obj, [mediaSegment.data]); // data: ArrayBuffer
    }
    function onLoadingComplete() {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].LOADING_COMPLETE
        };
        self.postMessage(obj);
    }
    function onRecoveredEarlyEof() {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].RECOVERED_EARLY_EOF
        };
        self.postMessage(obj);
    }
    function onMediaInfo(mediaInfo) {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].MEDIA_INFO,
            data: mediaInfo
        };
        self.postMessage(obj);
    }
    function onMetaDataArrived(metadata) {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].METADATA_ARRIVED,
            data: metadata
        };
        self.postMessage(obj);
    }
    function onScriptDataArrived(data) {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].SCRIPTDATA_ARRIVED,
            data: data
        };
        self.postMessage(obj);
    }
    function onTimedID3MetadataArrived(data) {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].TIMED_ID3_METADATA_ARRIVED,
            data: data
        };
        self.postMessage(obj);
    }
    function onPGSSubtitleDataArrived(data) {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].PGS_SUBTITLE_ARRIVED,
            data: data
        };
        self.postMessage(obj);
    }
    function onSynchronousKLVMetadataArrived(data) {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].SYNCHRONOUS_KLV_METADATA_ARRIVED,
            data: data
        };
        self.postMessage(obj);
    }
    function onAsynchronousKLVMetadataArrived(data) {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].ASYNCHRONOUS_KLV_METADATA_ARRIVED,
            data: data
        };
        self.postMessage(obj);
    }
    function onSMPTE2038MetadataArrived(data) {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].SMPTE2038_METADATA_ARRIVED,
            data: data
        };
        self.postMessage(obj);
    }
    function onSCTE35MetadataArrived(data) {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].SCTE35_METADATA_ARRIVED,
            data: data
        };
        self.postMessage(obj);
    }
    function onPESPrivateDataDescriptor(data) {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].PES_PRIVATE_DATA_DESCRIPTOR,
            data: data
        };
        self.postMessage(obj);
    }
    function onPESPrivateDataArrived(data) {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].PES_PRIVATE_DATA_ARRIVED,
            data: data
        };
        self.postMessage(obj);
    }
    function onStatisticsInfo(statInfo) {
        var obj = {
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].STATISTICS_INFO,
            data: statInfo
        };
        self.postMessage(obj);
    }
    function onIOError(type, info) {
        self.postMessage({
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].IO_ERROR,
            data: {
                type: type,
                info: info
            }
        });
    }
    function onDemuxError(type, info) {
        self.postMessage({
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].DEMUX_ERROR,
            data: {
                type: type,
                info: info
            }
        });
    }
    function onRecommendSeekpoint(milliseconds) {
        self.postMessage({
            msg: _transmuxing_events__WEBPACK_IMPORTED_MODULE_4__["default"].RECOMMEND_SEEKPOINT,
            data: milliseconds
        });
    }
    function onLogcatCallback(type, str) {
        self.postMessage({
            msg: 'logcat_callback',
            data: {
                type: type,
                logcat: str
            }
        });
    }
};
/* harmony default export */ __webpack_exports__["default"] = (TransmuxingWorker);


/***/ }),

/***/ "./src/demux/aac.ts":
/*!**************************!*\
  !*** ./src/demux/aac.ts ***!
  \**************************/
/*! exports provided: AACFrame, LOASAACFrame, AACADTSParser, AACLOASParser, AudioSpecificConfig */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "AACFrame", function() { return AACFrame; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "LOASAACFrame", function() { return LOASAACFrame; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "AACADTSParser", function() { return AACADTSParser; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "AACLOASParser", function() { return AACLOASParser; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "AudioSpecificConfig", function() { return AudioSpecificConfig; });
/* harmony import */ var _utils_logger__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger */ "./src/utils/logger.js");
/* harmony import */ var _exp_golomb__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ./exp-golomb */ "./src/demux/exp-golomb.js");
/* harmony import */ var _mpeg4_audio__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ./mpeg4-audio */ "./src/demux/mpeg4-audio.ts");
var __extends = (undefined && undefined.__extends) || (function () {
    var extendStatics = function (d, b) {
        extendStatics = Object.setPrototypeOf ||
            ({ __proto__: [] } instanceof Array && function (d, b) { d.__proto__ = b; }) ||
            function (d, b) { for (var p in b) if (Object.prototype.hasOwnProperty.call(b, p)) d[p] = b[p]; };
        return extendStatics(d, b);
    };
    return function (d, b) {
        if (typeof b !== "function" && b !== null)
            throw new TypeError("Class extends value " + String(b) + " is not a constructor or null");
        extendStatics(d, b);
        function __() { this.constructor = d; }
        d.prototype = b === null ? Object.create(b) : (__.prototype = b.prototype, new __());
    };
})();



var AACFrame = /** @class */ (function () {
    function AACFrame() {
    }
    return AACFrame;
}());

var LOASAACFrame = /** @class */ (function (_super) {
    __extends(LOASAACFrame, _super);
    function LOASAACFrame() {
        return _super !== null && _super.apply(this, arguments) || this;
    }
    return LOASAACFrame;
}(AACFrame));

var AACADTSParser = /** @class */ (function () {
    function AACADTSParser(data) {
        this.TAG = "AACADTSParser";
        this.data_ = data;
        this.current_syncword_offset_ = this.findNextSyncwordOffset(0);
        if (this.eof_flag_) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "Could not found ADTS syncword until payload end");
        }
    }
    AACADTSParser.prototype.findNextSyncwordOffset = function (syncword_offset) {
        var i = syncword_offset;
        var data = this.data_;
        while (true) {
            if (i + 7 >= data.byteLength) {
                this.eof_flag_ = true;
                return data.byteLength;
            }
            // search 12-bit 0xFFF syncword
            var syncword = ((data[i + 0] << 8) | data[i + 1]) >>> 4;
            if (syncword === 0xFFF) {
                return i;
            }
            else {
                i++;
            }
        }
    };
    AACADTSParser.prototype.readNextAACFrame = function () {
        var data = this.data_;
        var aac_frame = null;
        while (aac_frame == null) {
            if (this.eof_flag_) {
                break;
            }
            var syncword_offset = this.current_syncword_offset_;
            var offset = syncword_offset;
            // adts_fixed_header()
            // syncword 0xFFF: 12-bit
            var ID = (data[offset + 1] & 0x08) >>> 3;
            var layer = (data[offset + 1] & 0x06) >>> 1;
            var protection_absent = data[offset + 1] & 0x01;
            var profile = (data[offset + 2] & 0xC0) >>> 6;
            var sampling_frequency_index = (data[offset + 2] & 0x3C) >>> 2;
            var channel_configuration = ((data[offset + 2] & 0x01) << 2)
                | ((data[offset + 3] & 0xC0) >>> 6);
            // adts_variable_header()
            var aac_frame_length = ((data[offset + 3] & 0x03) << 11)
                | (data[offset + 4] << 3)
                | ((data[offset + 5] & 0xE0) >>> 5);
            var number_of_raw_data_blocks_in_frame = data[offset + 6] & 0x03;
            if (offset + aac_frame_length > this.data_.byteLength) {
                // data not enough for extracting last sample
                this.eof_flag_ = true;
                this.has_last_incomplete_data = true;
                break;
            }
            var adts_header_length = (protection_absent === 1) ? 7 : 9;
            var adts_frame_payload_length = aac_frame_length - adts_header_length;
            offset += adts_header_length;
            var next_syncword_offset = this.findNextSyncwordOffset(offset + adts_frame_payload_length);
            this.current_syncword_offset_ = next_syncword_offset;
            if ((ID !== 0 && ID !== 1) || layer !== 0) {
                // invalid adts frame ?
                continue;
            }
            var frame_data = data.subarray(offset, offset + adts_frame_payload_length);
            aac_frame = new AACFrame();
            aac_frame.audio_object_type = (profile + 1);
            aac_frame.sampling_freq_index = sampling_frequency_index;
            aac_frame.sampling_frequency = _mpeg4_audio__WEBPACK_IMPORTED_MODULE_2__["MPEG4SamplingFrequencies"][sampling_frequency_index];
            aac_frame.channel_config = channel_configuration;
            aac_frame.data = frame_data;
        }
        return aac_frame;
    };
    AACADTSParser.prototype.hasIncompleteData = function () {
        return this.has_last_incomplete_data;
    };
    AACADTSParser.prototype.getIncompleteData = function () {
        if (!this.has_last_incomplete_data) {
            return null;
        }
        return this.data_.subarray(this.current_syncword_offset_);
    };
    return AACADTSParser;
}());

var AACLOASParser = /** @class */ (function () {
    function AACLOASParser(data) {
        this.TAG = "AACLOASParser";
        this.data_ = data;
        this.current_syncword_offset_ = this.findNextSyncwordOffset(0);
        if (this.eof_flag_) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "Could not found LOAS syncword until payload end");
        }
    }
    AACLOASParser.prototype.findNextSyncwordOffset = function (syncword_offset) {
        var i = syncword_offset;
        var data = this.data_;
        while (true) {
            if (i + 1 >= data.byteLength) {
                this.eof_flag_ = true;
                return data.byteLength;
            }
            // search 12-bit 0xFFF syncword
            var syncword = (data[i + 0] << 3) | (data[i + 1] >>> 5);
            if (syncword === 0x2B7) {
                return i;
            }
            else {
                i++;
            }
        }
    };
    AACLOASParser.prototype.getLATMValue = function (gb) {
        var bytesForValue = gb.readBits(2);
        var value = 0;
        for (var i = 0; i <= bytesForValue; i++) {
            value = value << 8;
            value = value | gb.readByte();
        }
        return value;
    };
    AACLOASParser.prototype.readNextAACFrame = function (privious) {
        var data = this.data_;
        var aac_frame = null;
        while (aac_frame == null) {
            if (this.eof_flag_) {
                break;
            }
            var syncword_offset = this.current_syncword_offset_;
            var offset = syncword_offset;
            var audioMuxLengthBytes = ((data[offset + 1] & 0x1F) << 8) | data[offset + 2];
            if (offset + 3 + audioMuxLengthBytes >= this.data_.byteLength) {
                // data not enough for extracting last sample
                this.eof_flag_ = true;
                this.has_last_incomplete_data = true;
                break;
            }
            // AudioMuxElement(1)
            var gb = new _exp_golomb__WEBPACK_IMPORTED_MODULE_1__["default"](data.subarray(offset + 3, offset + 3 + audioMuxLengthBytes));
            var useSameStreamMux = gb.readBool();
            var streamMuxConfig = null;
            if (!useSameStreamMux) {
                var audioMuxVersion = gb.readBool();
                var audioMuxVersionA = audioMuxVersion && gb.readBool();
                if (audioMuxVersionA) {
                    _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, 'audioMuxVersionA is Not Supported');
                    gb.destroy();
                    break;
                }
                if (audioMuxVersion) {
                    this.getLATMValue(gb);
                }
                var allStreamsSameTimeFraming = gb.readBool();
                if (!allStreamsSameTimeFraming) {
                    _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, 'allStreamsSameTimeFraming zero is Not Supported');
                    gb.destroy();
                    break;
                }
                var numSubFrames = gb.readBits(6);
                if (numSubFrames !== 0) {
                    _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, 'more than 2 numSubFrames Not Supported');
                    gb.destroy();
                    break;
                }
                var numProgram = gb.readBits(4);
                if (numProgram !== 0) {
                    _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, 'more than 2 numProgram Not Supported');
                    gb.destroy();
                    break;
                }
                var numLayer = gb.readBits(3);
                if (numLayer !== 0) {
                    _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, 'more than 2 numLayer Not Supported');
                    gb.destroy();
                    break;
                }
                var fillBits = audioMuxVersion ? this.getLATMValue(gb) : 0;
                var audio_object_type = gb.readBits(5);
                fillBits -= 5;
                var sampling_freq_index = gb.readBits(4);
                fillBits -= 4;
                var channel_config = gb.readBits(4);
                fillBits -= 4;
                gb.readBits(3);
                fillBits -= 3; // GA Specfic Config
                if (fillBits > 0) {
                    gb.readBits(fillBits);
                }
                var frameLengthType = gb.readBits(3);
                if (frameLengthType === 0) {
                    gb.readByte();
                }
                else {
                    _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "frameLengthType = ".concat(frameLengthType, ". Only frameLengthType = 0 Supported"));
                    gb.destroy();
                    break;
                }
                var otherDataPresent = gb.readBool();
                if (otherDataPresent) {
                    if (audioMuxVersion) {
                        this.getLATMValue(gb);
                    }
                    else {
                        var otherDataLenBits = 0;
                        while (true) {
                            otherDataLenBits = otherDataLenBits << 8;
                            var otherDataLenEsc = gb.readBool();
                            var otherDataLenTmp = gb.readByte();
                            otherDataLenBits += otherDataLenTmp;
                            if (!otherDataLenEsc) {
                                break;
                            }
                        }
                        console.log(otherDataLenBits);
                    }
                }
                var crcCheckPresent = gb.readBool();
                if (crcCheckPresent) {
                    gb.readByte();
                }
                streamMuxConfig = new LOASAACFrame();
                streamMuxConfig.audio_object_type = audio_object_type;
                streamMuxConfig.sampling_freq_index = sampling_freq_index;
                streamMuxConfig.sampling_frequency = _mpeg4_audio__WEBPACK_IMPORTED_MODULE_2__["MPEG4SamplingFrequencies"][streamMuxConfig.sampling_freq_index];
                streamMuxConfig.channel_config = channel_config;
                streamMuxConfig.other_data_present = otherDataPresent;
            }
            else if (privious == null) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'StreamMuxConfig Missing');
                this.current_syncword_offset_ = this.findNextSyncwordOffset(offset + 3 + audioMuxLengthBytes);
                gb.destroy();
                continue;
            }
            else {
                streamMuxConfig = privious;
            }
            var length_1 = 0;
            while (true) {
                var tmp = gb.readByte();
                length_1 += tmp;
                if (tmp !== 0xFF) {
                    break;
                }
            }
            var aac_data = new Uint8Array(length_1);
            for (var i = 0; i < length_1; i++) {
                aac_data[i] = gb.readByte();
            }
            aac_frame = new LOASAACFrame();
            aac_frame.audio_object_type = (streamMuxConfig.audio_object_type);
            aac_frame.sampling_freq_index = (streamMuxConfig.sampling_freq_index);
            aac_frame.sampling_frequency = _mpeg4_audio__WEBPACK_IMPORTED_MODULE_2__["MPEG4SamplingFrequencies"][streamMuxConfig.sampling_freq_index];
            aac_frame.channel_config = streamMuxConfig.channel_config;
            aac_frame.other_data_present = streamMuxConfig.other_data_present;
            aac_frame.data = aac_data;
            this.current_syncword_offset_ = this.findNextSyncwordOffset(offset + 3 + audioMuxLengthBytes);
        }
        return aac_frame;
    };
    AACLOASParser.prototype.hasIncompleteData = function () {
        return this.has_last_incomplete_data;
    };
    AACLOASParser.prototype.getIncompleteData = function () {
        if (!this.has_last_incomplete_data) {
            return null;
        }
        return this.data_.subarray(this.current_syncword_offset_);
    };
    return AACLOASParser;
}());

var AudioSpecificConfig = /** @class */ (function () {
    function AudioSpecificConfig(frame) {
        var config = null;
        var original_audio_object_type = frame.audio_object_type;
        var audio_object_type = frame.audio_object_type;
        var sampling_index = frame.sampling_freq_index;
        var channel_config = frame.channel_config;
        var extension_sampling_index = 0;
        var userAgent = navigator.userAgent.toLowerCase();
        if (userAgent.indexOf('firefox') !== -1) {
            // firefox: use SBR (HE-AAC) if freq less than 24kHz
            if (sampling_index >= 6) {
                audio_object_type = 5;
                config = new Array(4);
                extension_sampling_index = sampling_index - 3;
            }
            else { // use LC-AAC
                audio_object_type = 2;
                config = new Array(2);
                extension_sampling_index = sampling_index;
            }
        }
        else if (userAgent.indexOf('android') !== -1) {
            // android: always use LC-AAC
            audio_object_type = 2;
            config = new Array(2);
            extension_sampling_index = sampling_index;
        }
        else {
            // for other browsers, e.g. chrome...
            // Always use HE-AAC to make it easier to switch aac codec profile
            audio_object_type = 5;
            extension_sampling_index = sampling_index;
            config = new Array(4);
            if (sampling_index >= 6) {
                extension_sampling_index = sampling_index - 3;
            }
            else if (channel_config === 1) { // Mono channel
                audio_object_type = 2;
                config = new Array(2);
                extension_sampling_index = sampling_index;
            }
        }
        config[0] = audio_object_type << 3;
        config[0] |= (sampling_index & 0x0F) >>> 1;
        config[1] = (sampling_index & 0x0F) << 7;
        config[1] |= (channel_config & 0x0F) << 3;
        if (audio_object_type === 5) {
            config[1] |= ((extension_sampling_index & 0x0F) >>> 1);
            config[2] = (extension_sampling_index & 0x01) << 7;
            // extended audio object type: force to 2 (LC-AAC)
            config[2] |= (2 << 2);
            config[3] = 0;
        }
        this.config = config;
        this.sampling_rate = _mpeg4_audio__WEBPACK_IMPORTED_MODULE_2__["MPEG4SamplingFrequencies"][sampling_index];
        this.channel_count = channel_config;
        this.codec_mimetype = 'mp4a.40.' + audio_object_type;
        this.original_codec_mimetype = 'mp4a.40.' + original_audio_object_type;
    }
    return AudioSpecificConfig;
}());



/***/ }),

/***/ "./src/demux/ac3.ts":
/*!**************************!*\
  !*** ./src/demux/ac3.ts ***!
  \**************************/
/*! exports provided: AC3Frame, AC3Parser, AC3Config, EAC3Frame, EAC3Parser, EAC3Config */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "AC3Frame", function() { return AC3Frame; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "AC3Parser", function() { return AC3Parser; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "AC3Config", function() { return AC3Config; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "EAC3Frame", function() { return EAC3Frame; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "EAC3Parser", function() { return EAC3Parser; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "EAC3Config", function() { return EAC3Config; });
/* harmony import */ var _utils_logger__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger */ "./src/utils/logger.js");
/* harmony import */ var _exp_golomb__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ./exp-golomb */ "./src/demux/exp-golomb.js");


var AC3Frame = /** @class */ (function () {
    function AC3Frame() {
    }
    return AC3Frame;
}());

var frame_size_code_table = [
    [
        64, 64, 80, 80, 96, 96, 112, 112, 128, 128,
        160, 160, 192, 192, 224, 224, 256, 256, 320, 320,
        384, 384, 448, 448, 512, 512, 640, 640, 768, 768,
        896, 896, 1024, 1024, 1152, 1152, 1280, 1280,
    ],
    [
        69, 70, 87, 88, 104, 105, 121, 122, 139, 140,
        174, 175, 208, 209, 243, 244, 278, 279, 348, 349,
        417, 418, 487, 488, 557, 558, 696, 697, 835, 836,
        975, 976, 1114, 1115, 1253, 1254, 1393, 1394
    ],
    [
        96, 96, 120, 120, 144, 144, 168, 168, 192, 192,
        240, 240, 288, 288, 336, 336, 384, 384, 480, 480,
        576, 576, 672, 672, 768, 768, 960, 960, 1152, 1152,
        1344, 1344, 1536, 1536, 1728, 1728, 1920, 1920,
    ],
];
var AC3Parser = /** @class */ (function () {
    function AC3Parser(data) {
        this.TAG = "AC3Parser";
        this.data_ = data;
        this.current_syncword_offset_ = this.findNextSyncwordOffset(0);
        if (this.eof_flag_) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "Could not found AC3 syncword until payload end");
        }
    }
    AC3Parser.prototype.findNextSyncwordOffset = function (syncword_offset) {
        var i = syncword_offset;
        var data = this.data_;
        while (true) {
            if (i + 7 >= data.byteLength) {
                this.eof_flag_ = true;
                return data.byteLength;
            }
            // search 16-bit 0x0B77 syncword
            var syncword = (data[i + 0] << 8) | (data[i + 1] << 0);
            if (syncword === 0x0B77) {
                return i;
            }
            else {
                i++;
            }
        }
    };
    AC3Parser.prototype.readNextAC3Frame = function () {
        var data = this.data_;
        var ac3_frame = null;
        while (ac3_frame == null) {
            if (this.eof_flag_) {
                break;
            }
            var syncword_offset = this.current_syncword_offset_;
            var offset = syncword_offset;
            var sampling_rate_code = data[offset + 4] >> 6;
            var sampling_frequency = [48000, 44200, 33000][sampling_rate_code];
            var frame_size_code = data[offset + 4] & 0x3F;
            var frame_size = frame_size_code_table[sampling_rate_code][frame_size_code] * 2;
            if (isNaN(frame_size) || offset + frame_size > this.data_.byteLength) {
                // data not enough for extracting last sample
                this.eof_flag_ = true;
                this.has_last_incomplete_data = true;
                break;
            }
            var next_syncword_offset = this.findNextSyncwordOffset(offset + frame_size);
            this.current_syncword_offset_ = next_syncword_offset;
            var bit_stream_identification = data[offset + 5] >> 3;
            var bit_stream_mode = data[offset + 5] & 0x07;
            var channel_mode = data[offset + 6] >> 5;
            var lfe_skip = 0;
            if ((channel_mode & 0x01) !== 0 && channel_mode !== 1) {
                lfe_skip += 2;
            }
            if ((channel_mode & 0x04) !== 0) {
                lfe_skip += 2;
            }
            if (channel_mode === 0x02) {
                lfe_skip += 2;
            }
            var low_frequency_effects_channel_on = (((data[offset + 6] << 8) | (data[offset + 7] << 0)) >> (12 - lfe_skip)) & 0x01;
            var channel_count = [2, 1, 2, 3, 3, 4, 4, 5][channel_mode] + low_frequency_effects_channel_on;
            ac3_frame = new AC3Frame();
            ac3_frame.sampling_frequency = sampling_frequency;
            ac3_frame.channel_count = channel_count;
            ac3_frame.channel_mode = channel_mode;
            ac3_frame.bit_stream_identification = bit_stream_identification;
            ac3_frame.low_frequency_effects_channel_on = low_frequency_effects_channel_on;
            ac3_frame.bit_stream_mode = bit_stream_mode;
            ac3_frame.frame_size_code = frame_size_code;
            ac3_frame.data = data.subarray(offset, offset + frame_size);
        }
        return ac3_frame;
    };
    AC3Parser.prototype.hasIncompleteData = function () {
        return this.has_last_incomplete_data;
    };
    AC3Parser.prototype.getIncompleteData = function () {
        if (!this.has_last_incomplete_data) {
            return null;
        }
        return this.data_.subarray(this.current_syncword_offset_);
    };
    return AC3Parser;
}());

var AC3Config = /** @class */ (function () {
    function AC3Config(frame) {
        var config = null;
        config = [
            (frame.sampling_rate_code << 6) | (frame.bit_stream_identification << 1) | (frame.bit_stream_mode >> 2),
            ((frame.bit_stream_mode & 0x03) << 6) | (frame.channel_mode << 3) | (frame.low_frequency_effects_channel_on << 2) | (frame.frame_size_code >> 4),
            (frame.frame_size_code << 4) & 0xE0,
        ];
        this.config = config;
        this.sampling_rate = frame.sampling_frequency;
        this.bit_stream_identification = frame.bit_stream_identification;
        this.bit_stream_mode = frame.bit_stream_mode;
        this.low_frequency_effects_channel_on = frame.low_frequency_effects_channel_on;
        this.channel_count = frame.channel_count;
        this.channel_mode = frame.channel_mode;
        this.codec_mimetype = 'ac-3';
        this.original_codec_mimetype = 'ac-3';
    }
    return AC3Config;
}());

var EAC3Frame = /** @class */ (function () {
    function EAC3Frame() {
    }
    return EAC3Frame;
}());

var EAC3Parser = /** @class */ (function () {
    function EAC3Parser(data) {
        this.TAG = "EAC3Parser";
        this.data_ = data;
        this.current_syncword_offset_ = this.findNextSyncwordOffset(0);
        if (this.eof_flag_) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "Could not found AC3 syncword until payload end");
        }
    }
    EAC3Parser.prototype.findNextSyncwordOffset = function (syncword_offset) {
        var i = syncword_offset;
        var data = this.data_;
        while (true) {
            if (i + 7 >= data.byteLength) {
                this.eof_flag_ = true;
                return data.byteLength;
            }
            // search 16-bit 0x0B77 syncword
            var syncword = (data[i + 0] << 8) | (data[i + 1] << 0);
            if (syncword === 0x0B77) {
                return i;
            }
            else {
                i++;
            }
        }
    };
    EAC3Parser.prototype.readNextEAC3Frame = function () {
        var data = this.data_;
        var eac3_frame = null;
        while (eac3_frame == null) {
            if (this.eof_flag_) {
                break;
            }
            var syncword_offset = this.current_syncword_offset_;
            var offset = syncword_offset;
            var gb = new _exp_golomb__WEBPACK_IMPORTED_MODULE_1__["default"](data.subarray(offset + 2));
            var stream_type = gb.readBits(2);
            var sub_stream_id = gb.readBits(3);
            var frame_size = (gb.readBits(11) + 1) << 1;
            var sampling_rate_code = gb.readBits(2);
            var sampling_frequency = null;
            var num_blocks_code = null;
            if (sampling_rate_code === 0x03) {
                sampling_rate_code = gb.readBits(2);
                sampling_frequency = [24000, 22060, 16000][sampling_rate_code];
                num_blocks_code = 3;
            }
            else {
                sampling_frequency = [48000, 44100, 32000][sampling_rate_code];
                num_blocks_code = gb.readBits(2);
            }
            var channel_mode = gb.readBits(3);
            var low_frequency_effects_channel_on = gb.readBits(1);
            var bit_stream_identification = gb.readBits(5);
            if (offset + frame_size > this.data_.byteLength) {
                // data not enough for extracting last sample
                this.eof_flag_ = true;
                this.has_last_incomplete_data = true;
                break;
            }
            var next_syncword_offset = this.findNextSyncwordOffset(offset + frame_size);
            this.current_syncword_offset_ = next_syncword_offset;
            var channel_count = [2, 1, 2, 3, 3, 4, 4, 5][channel_mode] + low_frequency_effects_channel_on;
            gb.destroy();
            eac3_frame = new EAC3Frame();
            eac3_frame.sampling_frequency = sampling_frequency;
            eac3_frame.channel_count = channel_count;
            eac3_frame.channel_mode = channel_mode;
            eac3_frame.bit_stream_identification = bit_stream_identification;
            eac3_frame.low_frequency_effects_channel_on = low_frequency_effects_channel_on;
            eac3_frame.frame_size = frame_size;
            eac3_frame.num_blks = [1, 2, 3, 6][num_blocks_code];
            eac3_frame.data = data.subarray(offset, offset + frame_size);
        }
        return eac3_frame;
    };
    EAC3Parser.prototype.hasIncompleteData = function () {
        return this.has_last_incomplete_data;
    };
    EAC3Parser.prototype.getIncompleteData = function () {
        if (!this.has_last_incomplete_data) {
            return null;
        }
        return this.data_.subarray(this.current_syncword_offset_);
    };
    return EAC3Parser;
}());

var EAC3Config = /** @class */ (function () {
    function EAC3Config(frame) {
        var config = null;
        var data_rate_sub = Math.floor((frame.frame_size * frame.sampling_frequency) / (frame.num_blks * 16));
        config = [
            (data_rate_sub & 0x1FE0 >> 5),
            (data_rate_sub & 0x001F << 3),
            (frame.sampling_rate_code << 6) | (frame.bit_stream_identification << 1) | (0 << 0),
            (0 << 7) | (0 << 4) | (frame.channel_mode << 1) | (frame.low_frequency_effects_channel_on << 0),
            (0 << 5) | (0 << 1) | (0 << 0)
        ];
        this.config = config;
        this.sampling_rate = frame.sampling_frequency;
        this.bit_stream_identification = frame.bit_stream_identification;
        this.num_blks = frame.num_blks;
        this.low_frequency_effects_channel_on = frame.low_frequency_effects_channel_on;
        this.channel_count = frame.channel_count;
        this.channel_mode = frame.channel_mode;
        this.codec_mimetype = 'ec-3';
        this.original_codec_mimetype = 'ec-3';
    }
    return EAC3Config;
}());



/***/ }),

/***/ "./src/demux/amf-parser.js":
/*!*********************************!*\
  !*** ./src/demux/amf-parser.js ***!
  \*********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/* harmony import */ var _utils_utf8_conv_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ../utils/utf8-conv.js */ "./src/utils/utf8-conv.js");
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ../utils/exception.js */ "./src/utils/exception.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */



var le = (function () {
    var buf = new ArrayBuffer(2);
    (new DataView(buf)).setInt16(0, 256, true); // little-endian write
    return (new Int16Array(buf))[0] === 256; // platform-spec read, if equal then LE
})();
var AMF = /** @class */ (function () {
    function AMF() {
    }
    AMF.parseScriptData = function (arrayBuffer, dataOffset, dataSize) {
        var data = {};
        try {
            var name_1 = AMF.parseValue(arrayBuffer, dataOffset, dataSize);
            var value = AMF.parseValue(arrayBuffer, dataOffset + name_1.size, dataSize - name_1.size);
            data[name_1.data] = value.data;
        }
        catch (e) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].e('AMF', e.toString());
        }
        return data;
    };
    AMF.parseObject = function (arrayBuffer, dataOffset, dataSize) {
        if (dataSize < 3) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["IllegalStateException"]('Data not enough when parse ScriptDataObject');
        }
        var name = AMF.parseString(arrayBuffer, dataOffset, dataSize);
        var value = AMF.parseValue(arrayBuffer, dataOffset + name.size, dataSize - name.size);
        var isObjectEnd = value.objectEnd;
        return {
            data: {
                name: name.data,
                value: value.data
            },
            size: name.size + value.size,
            objectEnd: isObjectEnd
        };
    };
    AMF.parseVariable = function (arrayBuffer, dataOffset, dataSize) {
        return AMF.parseObject(arrayBuffer, dataOffset, dataSize);
    };
    AMF.parseString = function (arrayBuffer, dataOffset, dataSize) {
        if (dataSize < 2) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["IllegalStateException"]('Data not enough when parse String');
        }
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        var length = v.getUint16(0, !le);
        var str;
        if (length > 0) {
            str = Object(_utils_utf8_conv_js__WEBPACK_IMPORTED_MODULE_1__["default"])(new Uint8Array(arrayBuffer, dataOffset + 2, length));
        }
        else {
            str = '';
        }
        return {
            data: str,
            size: 2 + length
        };
    };
    AMF.parseLongString = function (arrayBuffer, dataOffset, dataSize) {
        if (dataSize < 4) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["IllegalStateException"]('Data not enough when parse LongString');
        }
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        var length = v.getUint32(0, !le);
        var str;
        if (length > 0) {
            str = Object(_utils_utf8_conv_js__WEBPACK_IMPORTED_MODULE_1__["default"])(new Uint8Array(arrayBuffer, dataOffset + 4, length));
        }
        else {
            str = '';
        }
        return {
            data: str,
            size: 4 + length
        };
    };
    AMF.parseDate = function (arrayBuffer, dataOffset, dataSize) {
        if (dataSize < 10) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["IllegalStateException"]('Data size invalid when parse Date');
        }
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        var timestamp = v.getFloat64(0, !le);
        var localTimeOffset = v.getInt16(8, !le);
        timestamp += localTimeOffset * 60 * 1000; // get UTC time
        return {
            data: new Date(timestamp),
            size: 8 + 2
        };
    };
    AMF.parseValue = function (arrayBuffer, dataOffset, dataSize) {
        if (dataSize < 1) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["IllegalStateException"]('Data not enough when parse Value');
        }
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        var offset = 1;
        var type = v.getUint8(0);
        var value;
        var objectEnd = false;
        try {
            switch (type) {
                case 0: // Number(Double) type
                    value = v.getFloat64(1, !le);
                    offset += 8;
                    break;
                case 1: { // Boolean type
                    var b = v.getUint8(1);
                    value = b ? true : false;
                    offset += 1;
                    break;
                }
                case 2: { // String type
                    var amfstr = AMF.parseString(arrayBuffer, dataOffset + 1, dataSize - 1);
                    value = amfstr.data;
                    offset += amfstr.size;
                    break;
                }
                case 3: { // Object(s) type
                    value = {};
                    var terminal = 0; // workaround for malformed Objects which has missing ScriptDataObjectEnd
                    if ((v.getUint32(dataSize - 4, !le) & 0x00FFFFFF) === 9) {
                        terminal = 3;
                    }
                    while (offset < dataSize - 4) { // 4 === type(UI8) + ScriptDataObjectEnd(UI24)
                        var amfobj = AMF.parseObject(arrayBuffer, dataOffset + offset, dataSize - offset - terminal);
                        if (amfobj.objectEnd)
                            break;
                        value[amfobj.data.name] = amfobj.data.value;
                        offset += amfobj.size;
                    }
                    if (offset <= dataSize - 3) {
                        var marker = v.getUint32(offset - 1, !le) & 0x00FFFFFF;
                        if (marker === 9) {
                            offset += 3;
                        }
                    }
                    break;
                }
                case 8: { // ECMA array type (Mixed array)
                    value = {};
                    offset += 4; // ECMAArrayLength(UI32)
                    var terminal = 0; // workaround for malformed MixedArrays which has missing ScriptDataObjectEnd
                    if ((v.getUint32(dataSize - 4, !le) & 0x00FFFFFF) === 9) {
                        terminal = 3;
                    }
                    while (offset < dataSize - 8) { // 8 === type(UI8) + ECMAArrayLength(UI32) + ScriptDataVariableEnd(UI24)
                        var amfvar = AMF.parseVariable(arrayBuffer, dataOffset + offset, dataSize - offset - terminal);
                        if (amfvar.objectEnd)
                            break;
                        value[amfvar.data.name] = amfvar.data.value;
                        offset += amfvar.size;
                    }
                    if (offset <= dataSize - 3) {
                        var marker = v.getUint32(offset - 1, !le) & 0x00FFFFFF;
                        if (marker === 9) {
                            offset += 3;
                        }
                    }
                    break;
                }
                case 9: // ScriptDataObjectEnd
                    value = undefined;
                    offset = 1;
                    objectEnd = true;
                    break;
                case 10: { // Strict array type
                    // ScriptDataValue[n]. NOTE: according to video_file_format_spec_v10_1.pdf
                    value = [];
                    var strictArrayLength = v.getUint32(1, !le);
                    offset += 4;
                    for (var i = 0; i < strictArrayLength; i++) {
                        var val = AMF.parseValue(arrayBuffer, dataOffset + offset, dataSize - offset);
                        value.push(val.data);
                        offset += val.size;
                    }
                    break;
                }
                case 11: { // Date type
                    var date = AMF.parseDate(arrayBuffer, dataOffset + 1, dataSize - 1);
                    value = date.data;
                    offset += date.size;
                    break;
                }
                case 12: { // Long string type
                    var amfLongStr = AMF.parseString(arrayBuffer, dataOffset + 1, dataSize - 1);
                    value = amfLongStr.data;
                    offset += amfLongStr.size;
                    break;
                }
                default:
                    // ignore and skip
                    offset = dataSize;
                    _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w('AMF', 'Unsupported AMF value type ' + type);
            }
        }
        catch (e) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].e('AMF', e.toString());
        }
        return {
            data: value,
            size: offset,
            objectEnd: objectEnd
        };
    };
    return AMF;
}());
/* harmony default export */ __webpack_exports__["default"] = (AMF);


/***/ }),

/***/ "./src/demux/av1-parser.ts":
/*!*********************************!*\
  !*** ./src/demux/av1-parser.ts ***!
  \*********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _exp_golomb_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ./exp-golomb.js */ "./src/demux/exp-golomb.js");
/*
 * Copyright (C) 2022 もにょてっく. All Rights Reserved.
 *
 * @author もにょ〜ん <monyone.teihen@gmail.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var __assign = (undefined && undefined.__assign) || function () {
    __assign = Object.assign || function(t) {
        for (var s, i = 1, n = arguments.length; i < n; i++) {
            s = arguments[i];
            for (var p in s) if (Object.prototype.hasOwnProperty.call(s, p))
                t[p] = s[p];
        }
        return t;
    };
    return __assign.apply(this, arguments);
};

var AV1OBUParser = /** @class */ (function () {
    function AV1OBUParser() {
    }
    AV1OBUParser.parseOBUs = function (uint8array, meta) {
        for (var i = 0; i < uint8array.byteLength;) {
            var first = i;
            var forbidden_bit = (uint8array[i] & 0x80) >> 7;
            var type = (uint8array[i] & 0x78) >> 3;
            var extension_flag = (uint8array[i] & 0x04) !== 0;
            var has_size_field = (uint8array[i] & 0x02) !== 0;
            var reserved_1bit = (uint8array[i] & 0x01) !== 0;
            i += 1;
            var temporal_id = 0, spatial_id = 0;
            if (extension_flag) {
                i += 1;
            }
            var size = Number.POSITIVE_INFINITY;
            if (has_size_field) {
                size = 0;
                for (var j = 0;; j++) {
                    var value = uint8array[i++];
                    size |= (value & 0x7F) << (j * 7);
                    if ((value & 0x80) === 0) {
                        break;
                    }
                }
            }
            console.log(type);
            if (type === 1) { // OBU_SEQUENCE_HEADER
                meta = __assign(__assign({}, AV1OBUParser.parseSeuqneceHeader(uint8array.subarray(i, i + size))), { sequence_header_data: uint8array.subarray(first, i + size) });
            }
            else if (type == 3 && meta) { // OBU_FRAME_HEADER
                meta = AV1OBUParser.parseOBUFrameHeader(uint8array.subarray(i, i + size), temporal_id, spatial_id, meta);
            }
            else if (type == 6 && meta) { // OBU_FRAME
                meta = AV1OBUParser.parseOBUFrameHeader(uint8array.subarray(i, i + size), temporal_id, spatial_id, meta);
            }
            i += size;
        }
        return meta;
    };
    AV1OBUParser.parseSeuqneceHeader = function (uint8array) {
        var gb = new _exp_golomb_js__WEBPACK_IMPORTED_MODULE_0__["default"](uint8array);
        var seq_profile = gb.readBits(3);
        var still_picture = gb.readBool();
        var reduced_still_picture_header = gb.readBool();
        var fps = 0, fps_fixed = true, fps_num = 0, fps_den = 1;
        var decoder_model_info_present_flag = false;
        var decoder_model_present_for_this_op = false;
        var buffer_delay_length_minus_1 = undefined;
        var buffer_removal_time_length_minus_1 = undefined;
        var operating_points = [];
        if (reduced_still_picture_header) {
            operating_points.push({
                operating_point_idc: 0,
                level: gb.readBits(5),
                tier: 0,
            });
        }
        else {
            var timing_info_present_flag = gb.readBool();
            if (timing_info_present_flag) {
                // timing_info
                var num_units_in_display_tick = gb.readBits(32);
                var time_scale = gb.readBits(32);
                var equal_picture_interval = gb.readBool();
                var num_ticks_per_picture_minus_1 = 0;
                if (equal_picture_interval) {
                    var leading = 0;
                    while (true) {
                        var value = gb.readBits(1);
                        if (value !== 0) {
                            break;
                        }
                        leading += 1;
                    }
                    if (leading >= 32) {
                        num_ticks_per_picture_minus_1 = 0xFFFFFFFF;
                    }
                    else {
                        num_ticks_per_picture_minus_1 = ((1 << leading) - 1) + gb.readBits(leading);
                    }
                }
                fps_den = num_units_in_display_tick;
                fps_num = time_scale;
                fps = fps_num / fps_den;
                fps_fixed = equal_picture_interval;
                var decoder_model_info_present_flag_1 = gb.readBool();
                if (decoder_model_info_present_flag_1) {
                    // decoder_model_info
                    buffer_delay_length_minus_1 = gb.readBits(5);
                    var num_units_in_decoding_tick = gb.readBits(32);
                    buffer_removal_time_length_minus_1 = gb.readBits(5);
                    var frame_presentation_time_length_minus_1 = gb.readBits(5);
                }
            }
            var initial_display_delay_present_flag = gb.readBool();
            var operating_points_cnt_minus_1 = gb.readBits(5);
            for (var i = 0; i <= operating_points_cnt_minus_1; i++) {
                var operating_point_idc = gb.readBits(12);
                var level_1 = gb.readBits(5);
                var tier_1 = level_1 > 7 ? gb.readBits(1) : 0;
                operating_points.push({
                    operating_point_idc: operating_point_idc,
                    level: level_1,
                    tier: tier_1
                });
                if (decoder_model_info_present_flag) {
                    var decoder_model_present_for_this_op_1 = gb.readBool();
                    operating_points[operating_points.length - 1].decoder_model_present_for_this_op = decoder_model_present_for_this_op_1;
                    if (decoder_model_present_for_this_op_1) {
                        // operating_parameters_info
                        var decoder_buffer_delay = gb.readBits(buffer_delay_length_minus_1 + 1);
                        var encoder_buffer_delay = gb.readBits(buffer_delay_length_minus_1 + 1);
                        var low_delay_mode_flag = gb.readBool();
                    }
                }
                if (initial_display_delay_present_flag) {
                    var initial_display_delay_present_for_this_op = gb.readBool();
                    if (initial_display_delay_present_for_this_op) {
                        var initial_display_delay_minus_1 = gb.readBits(4);
                    }
                }
            }
        }
        var operating_point = 0;
        var _a = operating_points[operating_point], level = _a.level, tier = _a.tier;
        var frame_width_bits_minus_1 = gb.readBits(4);
        var frame_height_bits_minus_1 = gb.readBits(4);
        var max_frame_width = gb.readBits(frame_width_bits_minus_1 + 1) + 1;
        var max_frame_height = gb.readBits(frame_height_bits_minus_1 + 1) + 1;
        var frame_id_numbers_present_flag = false;
        if (!reduced_still_picture_header) {
            frame_id_numbers_present_flag = gb.readBool();
        }
        var delta_frame_id_length_minus_2 = undefined;
        var additional_frame_id_length_minus_1 = undefined;
        if (frame_id_numbers_present_flag) {
            var delta_frame_id_length_minus_2_1 = gb.readBits(4);
            var additional_frame_id_length_minus_1_1 = gb.readBits(4);
        }
        var SELECT_SCREEN_CONTENT_TOOLS = 2;
        var SELECT_INTEGER_MV = 2;
        var use_128x128_superblock = gb.readBool();
        var enable_filter_intra = gb.readBool();
        var enable_intra_edge_filter = gb.readBool();
        var enable_interintra_compound = false;
        var enable_masked_compound = false;
        var enable_warped_motion = false;
        var enable_dual_filter = false;
        var enable_order_hint = false;
        var enable_jnt_comp = false;
        var enable_ref_frame_mvs = false;
        var seq_force_screen_content_tools = SELECT_SCREEN_CONTENT_TOOLS;
        var seq_force_integer_mv = SELECT_INTEGER_MV;
        var OrderHintBits = 0;
        if (!reduced_still_picture_header) {
            enable_interintra_compound = gb.readBool();
            enable_masked_compound = gb.readBool();
            enable_warped_motion = gb.readBool();
            enable_dual_filter = gb.readBool();
            enable_order_hint = gb.readBool();
            if (enable_order_hint) {
                var enable_jnt_comp_1 = gb.readBool();
                var enable_ref_frame_mvs_1 = gb.readBool();
            }
            var seq_choose_screen_content_tools = gb.readBool();
            if (seq_choose_screen_content_tools) {
                seq_force_screen_content_tools = SELECT_SCREEN_CONTENT_TOOLS;
            }
            else {
                seq_force_screen_content_tools = gb.readBits(1);
            }
            if (seq_force_screen_content_tools) {
                var seq_choose_integer_mv = gb.readBool();
                if (seq_choose_integer_mv) {
                    seq_force_integer_mv = SELECT_INTEGER_MV;
                }
                else {
                    seq_force_integer_mv = gb.readBits(1);
                }
            }
            else {
                seq_force_integer_mv = SELECT_INTEGER_MV;
            }
            if (enable_order_hint) {
                var order_hint_bits_minus_1 = gb.readBits(3);
                OrderHintBits = order_hint_bits_minus_1 + 1;
            }
            else {
                OrderHintBits = 0;
            }
        }
        var enable_superres = gb.readBool();
        var enable_cdef = gb.readBool();
        var enable_restoration = gb.readBool();
        // color_config
        var high_bitdepth = gb.readBool();
        var bitDepth = 8;
        if (seq_profile === 2 && high_bitdepth) {
            var twelve_bit = gb.readBool();
            bitDepth = twelve_bit ? 12 : 10;
        }
        else {
            bitDepth = high_bitdepth ? 10 : 8;
        }
        var mono_chrome = false;
        if (seq_profile !== 1) {
            mono_chrome = gb.readBool();
        }
        var numPlanes = mono_chrome ? 1 : 3;
        var color_description_present_flag = gb.readBool();
        var CP_BT_709 = 1, CP_UNSPECIFIED = 2;
        var TC_UNSPECIFIED = 2, TC_SRGB = 13;
        var MC_UNSPECIFIED = 2, MC_IDENTITY = 0;
        var color_primaries = CP_UNSPECIFIED;
        var transfer_characteristics = TC_UNSPECIFIED;
        var matrix_coefficients = MC_UNSPECIFIED;
        if (color_description_present_flag) {
            var color_primaries_1 = gb.readBits(8);
            var transfer_characteristics_1 = gb.readBits(8);
            var matrix_coefficients_1 = gb.readBits(8);
        }
        var color_range = 1;
        var subsampling_x = 1;
        var subsampling_y = 1;
        if (mono_chrome) {
            color_range = gb.readBits(1);
            subsampling_x = 1;
            subsampling_y = 1;
            var chroma_sample_position = 0; /* CSP_UNKNOWN */
            var separate_uv_delta_q = 0;
        }
        else {
            var color_range_1 = 1;
            if (color_primaries === CP_BT_709 && transfer_characteristics === TC_SRGB && matrix_coefficients === MC_IDENTITY) {
                color_range_1 = 1;
                subsampling_x = 1;
                subsampling_y = 1;
            }
            else {
                color_range_1 = gb.readBits(1);
                if (seq_profile == 0) {
                    subsampling_x = 1;
                    subsampling_y = 1;
                }
                else if (seq_profile == 1) {
                    subsampling_x = 0;
                    subsampling_y = 0;
                }
                else {
                    if (bitDepth == 12) {
                        var subsampling_x_1 = gb.readBits(1);
                        if (subsampling_x_1) {
                            var subsampling_y_1 = gb.readBits(1);
                        }
                        else {
                            var subsampling_y_2 = 0;
                        }
                    }
                    else {
                        subsampling_x = 1;
                        subsampling_y = 0;
                    }
                }
                if (subsampling_x && subsampling_y) {
                    var chroma_sample_position = gb.readBits(2);
                }
                var separate_uv_delta_q = gb.readBits(1);
            }
        }
        //
        var film_grain_params_present = gb.readBool();
        gb.destroy();
        gb = null;
        var codec_mimetype = "av01.".concat(seq_profile, ".").concat(AV1OBUParser.getLevelString(level, tier), ".").concat(bitDepth.toString(10).padStart(2, '0'));
        var sar_width = 1, sar_height = 1, sar_scale = 1;
        return {
            codec_mimetype: codec_mimetype,
            level: level,
            tier: tier,
            level_string: AV1OBUParser.getLevelString(level, tier),
            profile_idc: seq_profile,
            profile_string: "".concat(seq_profile),
            bit_depth: bitDepth,
            ref_frames: 1,
            chroma_format: AV1OBUParser.getChromaFormat(mono_chrome, subsampling_x, subsampling_y),
            chroma_format_string: AV1OBUParser.getChromaFormatString(mono_chrome, subsampling_x, subsampling_y),
            sequence_header: {
                frame_id_numbers_present_flag: frame_id_numbers_present_flag,
                additional_frame_id_length_minus_1: additional_frame_id_length_minus_1,
                delta_frame_id_length_minus_2: delta_frame_id_length_minus_2,
                reduced_still_picture_header: reduced_still_picture_header,
                decoder_model_info_present_flag: decoder_model_info_present_flag,
                operating_points: operating_points,
                buffer_removal_time_length_minus_1: buffer_removal_time_length_minus_1,
                equal_picture_interval: fps_fixed,
                seq_force_screen_content_tools: seq_force_screen_content_tools,
                seq_force_integer_mv: seq_force_integer_mv,
                enable_order_hint: enable_order_hint,
                order_hint_bits: OrderHintBits,
                enable_superres: enable_superres,
                frame_width_bit: frame_width_bits_minus_1 + 1,
                frame_height_bit: frame_height_bits_minus_1 + 1,
                max_frame_width: max_frame_width,
                max_frame_height: max_frame_height,
            },
            keyframe: undefined,
            frame_rate: {
                fixed: fps_fixed,
                fps: fps_num / fps_den,
                fps_den: fps_den,
                fps_num: fps_num,
            },
        };
    };
    AV1OBUParser.parseOBUFrameHeader = function (uint8array, temporal_id, spatial_id, meta) {
        var sequence_header = meta.sequence_header;
        var gb = new _exp_golomb_js__WEBPACK_IMPORTED_MODULE_0__["default"](uint8array);
        // obu_type is OBU_FRAME_HEADER, SeenFrameHeader = 0, OBU_REDUNDANT_FRAME_HEADER 1
        var NUM_REF_FRAMES = 8;
        var KEY_FRAME = 0;
        var INTER_FRAME = 1;
        var INTRA_ONLY_FRAME = 2;
        var SWITCH_FRAME = 3;
        var SELECT_SCREEN_CONTENT_TOOLS = 2;
        var SELECT_INTEGER_MV = 2;
        var PRIMARY_REF_NONE = 7;
        var FrameWidth = sequence_header.max_frame_width;
        var FrameHeight = sequence_header.max_frame_height;
        var RenderWidth = FrameWidth; // Stub
        var RenderHeight = FrameHeight; // Stub
        var idLen = 0;
        if (sequence_header.frame_id_numbers_present_flag) {
            idLen = sequence_header.additional_frame_id_length_minus_1 + sequence_header.delta_frame_id_length_minus_2 + 3;
        }
        var allFrames = (1 << NUM_REF_FRAMES) - 1;
        var show_existing_frame = false;
        var frame_type = 0;
        var keyframe = true;
        var show_frame = true;
        var showable_frame = false;
        var error_resilient_mode = false;
        if (!sequence_header.reduced_still_picture_header) {
            show_existing_frame = gb.readBool();
            if (show_existing_frame) {
                // it does not contain frame data. ignored
                return meta;
            }
            frame_type = gb.readBits(2);
            keyframe = frame_type === INTRA_ONLY_FRAME || frame_type === KEY_FRAME;
            show_frame = gb.readBool();
            if (show_frame && sequence_header.decoder_model_info_present_flag && !sequence_header.equal_picture_interval) {
                // decoder model info
            }
            if (!show_frame) {
                showable_frame = frame_type !== KEY_FRAME;
            }
            else {
                showable_frame = gb.readBool();
            }
            if (frame_type === SWITCH_FRAME || (frame_type === KEY_FRAME && show_frame)) {
                error_resilient_mode = true;
            }
            else {
                error_resilient_mode = gb.readBool();
            }
        }
        meta.keyframe = keyframe;
        var disable_cdf_update = gb.readBool();
        var allow_screen_content_tools = sequence_header.seq_force_screen_content_tools;
        if (sequence_header.seq_force_screen_content_tools === SELECT_SCREEN_CONTENT_TOOLS) {
            allow_screen_content_tools = gb.readBits(1);
        }
        var force_integer_mv = keyframe ? 1 : 0;
        if (allow_screen_content_tools) {
            force_integer_mv = sequence_header.seq_force_integer_mv;
            if (sequence_header.seq_force_integer_mv == SELECT_INTEGER_MV) {
                force_integer_mv = gb.readBits(1);
            }
        }
        var current_frame_id = 0;
        if (sequence_header.frame_id_numbers_present_flag) {
            current_frame_id = gb.readBits(idLen);
        }
        var frame_size_override_flag = false;
        if (frame_type == SWITCH_FRAME) {
            frame_size_override_flag = true;
        }
        else if (sequence_header.reduced_still_picture_header) {
            frame_size_override_flag = false;
        }
        else {
            frame_size_override_flag = gb.readBool();
        }
        var order_hint = gb.readBits(sequence_header.order_hint_bits);
        var primary_ref_frame = PRIMARY_REF_NONE;
        if (!(keyframe || error_resilient_mode)) {
            primary_ref_frame = gb.readBits(3);
        }
        if (sequence_header.decoder_model_info_present_flag) {
            var buffer_removal_time_present_flag = gb.readBool();
            if (buffer_removal_time_present_flag) {
                for (var opNum = 0; opNum <= sequence_header.operating_points_cnt_minus_1; opNum++) {
                    if (sequence_header.operating_points[opNum].decoder_model_present_for_this_op[opNum]) {
                        var opPtIdc = sequence_header.operating_points[opNum].operating_point_idc;
                        var inTemporalLayer = (opPtIdc >> temporal_id) & 1;
                        var inSpatialLayer = (opPtIdc >> (spatial_id + 8)) & 1;
                        if (opPtIdc === 0 || (inTemporalLayer && inSpatialLayer)) {
                            gb.readBits(sequence_header.buffer_removal_time_length_minus_1 + 1);
                        }
                    }
                }
            }
        }
        var allow_high_precision_mv = 0;
        var use_ref_frame_mvs = 0;
        var allow_intrabc = 0;
        var refresh_frame_flags = allFrames;
        if (!(frame_type === SWITCH_FRAME || (frame_type == KEY_FRAME && show_frame))) {
            refresh_frame_flags = gb.readBits(8);
        }
        if (keyframe || refresh_frame_flags !== allFrames) {
            if (error_resilient_mode && sequence_header.enable_order_hint) {
                for (var i = 0; i < NUM_REF_FRAMES; i++) {
                    gb.readBits(sequence_header.order_hint_bits);
                }
            }
        }
        if (keyframe) {
            var resolution = AV1OBUParser.frameSizeAndRenderSize(gb, frame_size_override_flag, sequence_header);
            meta.codec_size = {
                width: resolution.FrameWidth,
                height: resolution.FrameHeight,
            };
            meta.present_size = {
                width: resolution.RenderWidth,
                height: resolution.RenderHeight,
            };
            meta.sar_ratio = {
                width: resolution.RenderWidth / resolution.FrameWidth,
                height: resolution.RenderHeight / resolution.FrameHeight,
            };
        }
        // fmp4 can't support reference frame resolution change, so ignored
        gb.destroy();
        gb = null;
        return meta;
    };
    AV1OBUParser.frameSizeAndRenderSize = function (gb, frame_size_override_flag, sequence_header) {
        var FrameWidth = sequence_header.max_frame_width;
        var FrameHeight = sequence_header.max_frame_height;
        if (frame_size_override_flag) {
            FrameWidth = gb.readBits(sequence_header.frame_width_bit) + 1;
            FrameHeight = gb.readBits(sequence_header.frame_height_bit) + 1;
        }
        var use_superress = false;
        if (sequence_header.enable_superres) {
            use_superress = gb.readBool();
        }
        var SuperresDenom = 8 /* SUPERRES_NUM */;
        if (use_superress) {
            var coded_denom = gb.readBits(3 /* SUPERRES_DENOM_BITS */);
            SuperresDenom = coded_denom + 9; /* SUPERRES_DENOM_MIN */
        }
        var UpscaledWidth = FrameWidth;
        FrameWidth = Math.floor((UpscaledWidth * 8 /* SUPERRES_NUM */ + (SuperresDenom / 2)) / SuperresDenom);
        var render_and_frame_size_different = gb.readBool();
        var RenderWidth = UpscaledWidth;
        var RenderHeight = FrameHeight;
        if (render_and_frame_size_different) {
            var render_width_bits = gb.readBits(16) + 1;
            var render_height_bits = gb.readBits(16) + 1;
            RenderWidth = gb.readBits(render_width_bits) + 1;
            RenderHeight = gb.readBits(render_height_bits) + 1;
        }
        return {
            UpscaledWidth: UpscaledWidth,
            FrameWidth: FrameWidth,
            FrameHeight: FrameHeight,
            RenderWidth: RenderWidth,
            RenderHeight: RenderHeight
        };
    };
    AV1OBUParser.getLevelString = function (level, tier) {
        return "".concat(level.toString(10).padStart(2, '0')).concat(tier === 0 ? 'M' : 'H');
    };
    AV1OBUParser.getChromaFormat = function (mono_chrome, subsampling_x, subsampling_y) {
        if (mono_chrome) {
            return 0;
        }
        else if (subsampling_x === 0 && subsampling_y === 0) {
            return 3;
        }
        else if (subsampling_x === 1 && subsampling_y === 0) {
            return 2;
        }
        else if (subsampling_x === 1 && subsampling_y === 1) {
            return 1;
        }
        else {
            return Number.NaN;
        }
    };
    AV1OBUParser.getChromaFormatString = function (mono_chrome, subsampling_x, subsampling_y) {
        if (mono_chrome) {
            return '4:0:0';
        }
        else if (subsampling_x === 0 && subsampling_y === 0) {
            return '4:4:4';
        }
        else if (subsampling_x === 1 && subsampling_y === 0) {
            return '4:2:2';
        }
        else if (subsampling_x === 1 && subsampling_y === 1) {
            return '4:2:0';
        }
        else {
            return 'Unknown';
        }
    };
    return AV1OBUParser;
}());
/* harmony default export */ __webpack_exports__["default"] = (AV1OBUParser);


/***/ }),

/***/ "./src/demux/av1.ts":
/*!**************************!*\
  !*** ./src/demux/av1.ts ***!
  \**************************/
/*! exports provided: AV1OBUInMpegTsParser, default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "AV1OBUInMpegTsParser", function() { return AV1OBUInMpegTsParser; });
/* harmony import */ var _utils_logger__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger */ "./src/utils/logger.js");
/*
 * Copyright (C) 2023 もにょてっく. All Rights Reserved.
 *
 * @author もにょ〜ん <monyone.teihen@gmail.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

var AV1OBUInMpegTsParser = /** @class */ (function () {
    function AV1OBUInMpegTsParser(data) {
        this.TAG = "AV1OBUInMpegTsParser";
        this.current_startcode_offset_ = 0;
        this.eof_flag_ = false;
        this.data_ = data;
        this.current_startcode_offset_ = this.findNextStartCodeOffset(0);
        if (this.eof_flag_) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "Could not find AV1 startcode until payload end!");
        }
    }
    AV1OBUInMpegTsParser._ebsp2rbsp = function (uint8array) {
        var src = uint8array;
        var src_length = src.byteLength;
        var dst = new Uint8Array(src_length);
        var dst_idx = 0;
        for (var i = 0; i < src_length; i++) {
            if (i >= 2) {
                // Unescape: Skip 0x03 after 00 00
                if (src[i] === 0x03 && src[i - 1] === 0x00 && src[i - 2] === 0x00) {
                    continue;
                }
            }
            dst[dst_idx] = src[i];
            dst_idx++;
        }
        return new Uint8Array(dst.buffer, 0, dst_idx);
    };
    AV1OBUInMpegTsParser.prototype.findNextStartCodeOffset = function (start_offset) {
        var i = start_offset;
        var data = this.data_;
        while (true) {
            if (i + 2 >= data.byteLength) {
                this.eof_flag_ = true;
                return data.byteLength;
            }
            // search 00 00 01
            var uint24 = (data[i + 0] << 16)
                | (data[i + 1] << 8)
                | (data[i + 2]);
            if (uint24 === 0x000001) {
                return i;
            }
            else {
                i++;
            }
        }
    };
    AV1OBUInMpegTsParser.prototype.readNextOBUPayload = function () {
        var data = this.data_;
        var payload = null;
        while (payload == null) {
            if (this.eof_flag_) {
                break;
            }
            // offset pointed to start code
            var startcode_offset = this.current_startcode_offset_;
            // nalu payload start offset
            var offset = startcode_offset + 3;
            var next_startcode_offset = this.findNextStartCodeOffset(offset);
            this.current_startcode_offset_ = next_startcode_offset;
            payload = AV1OBUInMpegTsParser._ebsp2rbsp(data.subarray(offset, next_startcode_offset));
        }
        return payload;
    };
    return AV1OBUInMpegTsParser;
}());

/* harmony default export */ __webpack_exports__["default"] = (AV1OBUInMpegTsParser);


/***/ }),

/***/ "./src/demux/base-demuxer.ts":
/*!***********************************!*\
  !*** ./src/demux/base-demuxer.ts ***!
  \***********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
var BaseDemuxer = /** @class */ (function () {
    function BaseDemuxer() {
    }
    BaseDemuxer.prototype.destroy = function () {
        this.onError = null;
        this.onMediaInfo = null;
        this.onMetaDataArrived = null;
        this.onTrackMetadata = null;
        this.onDataAvailable = null;
        this.onTimedID3Metadata = null;
        this.onPGSSubtitleData = null;
        this.onSynchronousKLVMetadata = null;
        this.onAsynchronousKLVMetadata = null;
        this.onSMPTE2038Metadata = null;
        this.onSCTE35Metadata = null;
        this.onPESPrivateData = null;
        this.onPESPrivateDataDescriptor = null;
    };
    return BaseDemuxer;
}());
/* harmony default export */ __webpack_exports__["default"] = (BaseDemuxer);


/***/ }),

/***/ "./src/demux/demux-errors.js":
/*!***********************************!*\
  !*** ./src/demux/demux-errors.js ***!
  \***********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var DemuxErrors = {
    OK: 'OK',
    FORMAT_ERROR: 'FormatError',
    FORMAT_UNSUPPORTED: 'FormatUnsupported',
    CODEC_UNSUPPORTED: 'CodecUnsupported'
};
/* harmony default export */ __webpack_exports__["default"] = (DemuxErrors);


/***/ }),

/***/ "./src/demux/exp-golomb.js":
/*!*********************************!*\
  !*** ./src/demux/exp-golomb.js ***!
  \*********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/exception.js */ "./src/utils/exception.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Exponential-Golomb buffer decoder
var ExpGolomb = /** @class */ (function () {
    function ExpGolomb(uint8array) {
        this.TAG = 'ExpGolomb';
        this._buffer = uint8array;
        this._buffer_index = 0;
        this._total_bytes = uint8array.byteLength;
        this._total_bits = uint8array.byteLength * 8;
        this._current_word = 0;
        this._current_word_bits_left = 0;
    }
    ExpGolomb.prototype.destroy = function () {
        this._buffer = null;
    };
    ExpGolomb.prototype._fillCurrentWord = function () {
        var buffer_bytes_left = this._total_bytes - this._buffer_index;
        if (buffer_bytes_left <= 0)
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_0__["IllegalStateException"]('ExpGolomb: _fillCurrentWord() but no bytes available');
        var bytes_read = Math.min(4, buffer_bytes_left);
        var word = new Uint8Array(4);
        word.set(this._buffer.subarray(this._buffer_index, this._buffer_index + bytes_read));
        this._current_word = new DataView(word.buffer).getUint32(0, false);
        this._buffer_index += bytes_read;
        this._current_word_bits_left = bytes_read * 8;
    };
    ExpGolomb.prototype.readBits = function (bits) {
        if (bits > 32)
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_0__["InvalidArgumentException"]('ExpGolomb: readBits() bits exceeded max 32bits!');
        if (bits <= this._current_word_bits_left) {
            var result_1 = this._current_word >>> (32 - bits);
            this._current_word <<= bits;
            this._current_word_bits_left -= bits;
            return result_1;
        }
        var result = this._current_word_bits_left ? this._current_word : 0;
        result = result >>> (32 - this._current_word_bits_left);
        var bits_need_left = bits - this._current_word_bits_left;
        this._fillCurrentWord();
        var bits_read_next = Math.min(bits_need_left, this._current_word_bits_left);
        var result2 = this._current_word >>> (32 - bits_read_next);
        this._current_word <<= bits_read_next;
        this._current_word_bits_left -= bits_read_next;
        result = (result << bits_read_next) | result2;
        return result;
    };
    ExpGolomb.prototype.readBool = function () {
        return this.readBits(1) === 1;
    };
    ExpGolomb.prototype.readByte = function () {
        return this.readBits(8);
    };
    ExpGolomb.prototype._skipLeadingZero = function () {
        var zero_count;
        for (zero_count = 0; zero_count < this._current_word_bits_left; zero_count++) {
            if (0 !== (this._current_word & (0x80000000 >>> zero_count))) {
                this._current_word <<= zero_count;
                this._current_word_bits_left -= zero_count;
                return zero_count;
            }
        }
        this._fillCurrentWord();
        return zero_count + this._skipLeadingZero();
    };
    ExpGolomb.prototype.readUEG = function () {
        var leading_zeros = this._skipLeadingZero();
        return this.readBits(leading_zeros + 1) - 1;
    };
    ExpGolomb.prototype.readSEG = function () {
        var value = this.readUEG();
        if (value & 0x01) {
            return (value + 1) >>> 1;
        }
        else {
            return -1 * (value >>> 1);
        }
    };
    return ExpGolomb;
}());
/* harmony default export */ __webpack_exports__["default"] = (ExpGolomb);


/***/ }),

/***/ "./src/demux/flv-demuxer.js":
/*!**********************************!*\
  !*** ./src/demux/flv-demuxer.js ***!
  \**********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/* harmony import */ var _amf_parser_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ./amf-parser.js */ "./src/demux/amf-parser.js");
/* harmony import */ var _sps_parser_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ./sps-parser.js */ "./src/demux/sps-parser.js");
/* harmony import */ var _demux_errors_js__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ./demux-errors.js */ "./src/demux/demux-errors.js");
/* harmony import */ var _core_media_info_js__WEBPACK_IMPORTED_MODULE_4__ = __webpack_require__(/*! ../core/media-info.js */ "./src/core/media-info.js");
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_5__ = __webpack_require__(/*! ../utils/exception.js */ "./src/utils/exception.js");
/* harmony import */ var _h265_parser_js__WEBPACK_IMPORTED_MODULE_6__ = __webpack_require__(/*! ./h265-parser.js */ "./src/demux/h265-parser.js");
/* harmony import */ var _utils_typedarray_equality_ts__WEBPACK_IMPORTED_MODULE_7__ = __webpack_require__(/*! ../utils/typedarray-equality.ts */ "./src/utils/typedarray-equality.ts");
/* harmony import */ var _av1_parser_ts__WEBPACK_IMPORTED_MODULE_8__ = __webpack_require__(/*! ./av1-parser.ts */ "./src/demux/av1-parser.ts");
/* harmony import */ var _exp_golomb_js__WEBPACK_IMPORTED_MODULE_9__ = __webpack_require__(/*! ./exp-golomb.js */ "./src/demux/exp-golomb.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */










function Swap16(src) {
    return (((src >>> 8) & 0xFF) |
        ((src & 0xFF) << 8));
}
function Swap32(src) {
    return (((src & 0xFF000000) >>> 24) |
        ((src & 0x00FF0000) >>> 8) |
        ((src & 0x0000FF00) << 8) |
        ((src & 0x000000FF) << 24));
}
function ReadBig32(array, index) {
    return ((array[index] << 24) |
        (array[index + 1] << 16) |
        (array[index + 2] << 8) |
        (array[index + 3]));
}
var FLVDemuxer = /** @class */ (function () {
    function FLVDemuxer(probeData, config) {
        this.TAG = 'FLVDemuxer';
        this._config = config;
        this._onError = null;
        this._onMediaInfo = null;
        this._onMetaDataArrived = null;
        this._onScriptDataArrived = null;
        this._onTrackMetadata = null;
        this._onDataAvailable = null;
        this._dataOffset = probeData.dataOffset;
        this._firstParse = true;
        this._dispatch = false;
        this._hasAudio = probeData.hasAudioTrack;
        this._hasVideo = probeData.hasVideoTrack;
        this._hasAudioFlagOverrided = false;
        this._hasVideoFlagOverrided = false;
        this._audioInitialMetadataDispatched = false;
        this._videoInitialMetadataDispatched = false;
        this._mediaInfo = new _core_media_info_js__WEBPACK_IMPORTED_MODULE_4__["default"]();
        this._mediaInfo.hasAudio = this._hasAudio;
        this._mediaInfo.hasVideo = this._hasVideo;
        this._metadata = null;
        this._audioMetadata = null;
        this._videoMetadata = null;
        this._naluLengthSize = 4;
        this._timestampBase = 0; // int32, in milliseconds
        this._timescale = 1000;
        this._duration = 0; // int32, in milliseconds
        this._durationOverrided = false;
        this._referenceFrameRate = {
            fixed: true,
            fps: 23.976,
            fps_num: 23976,
            fps_den: 1000
        };
        this._flvSoundRateTable = [5500, 11025, 22050, 44100, 48000];
        this._mpegSamplingRates = [
            96000, 88200, 64000, 48000, 44100, 32000,
            24000, 22050, 16000, 12000, 11025, 8000, 7350
        ];
        this._mpegAudioV10SampleRateTable = [44100, 48000, 32000, 0];
        this._mpegAudioV20SampleRateTable = [22050, 24000, 16000, 0];
        this._mpegAudioV25SampleRateTable = [11025, 12000, 8000, 0];
        this._mpegAudioL1BitRateTable = [0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, -1];
        this._mpegAudioL2BitRateTable = [0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, -1];
        this._mpegAudioL3BitRateTable = [0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, -1];
        this._videoTrack = { type: 'video', id: 1, sequenceNumber: 0, samples: [], length: 0 };
        this._audioTrack = { type: 'audio', id: 2, sequenceNumber: 0, samples: [], length: 0 };
        this._littleEndian = (function () {
            var buf = new ArrayBuffer(2);
            (new DataView(buf)).setInt16(0, 256, true); // little-endian write
            return (new Int16Array(buf))[0] === 256; // platform-spec read, if equal then LE
        })();
    }
    FLVDemuxer.prototype.destroy = function () {
        this._mediaInfo = null;
        this._metadata = null;
        this._audioMetadata = null;
        this._videoMetadata = null;
        this._videoTrack = null;
        this._audioTrack = null;
        this._onError = null;
        this._onMediaInfo = null;
        this._onMetaDataArrived = null;
        this._onScriptDataArrived = null;
        this._onTrackMetadata = null;
        this._onDataAvailable = null;
    };
    FLVDemuxer.probe = function (buffer) {
        var data = new Uint8Array(buffer);
        if (data.byteLength < 9) {
            return { needMoreData: true };
        }
        var mismatch = { match: false };
        if (data[0] !== 0x46 || data[1] !== 0x4C || data[2] !== 0x56 || data[3] !== 0x01) {
            return mismatch;
        }
        var hasAudio = ((data[4] & 4) >>> 2) !== 0;
        var hasVideo = (data[4] & 1) !== 0;
        var offset = ReadBig32(data, 5);
        if (offset < 9) {
            return mismatch;
        }
        return {
            match: true,
            consumed: offset,
            dataOffset: offset,
            hasAudioTrack: hasAudio,
            hasVideoTrack: hasVideo
        };
    };
    FLVDemuxer.prototype.bindDataSource = function (loader) {
        loader.onDataArrival = this.parseChunks.bind(this);
        return this;
    };
    Object.defineProperty(FLVDemuxer.prototype, "onTrackMetadata", {
        // prototype: function(type: string, metadata: any): void
        get: function () {
            return this._onTrackMetadata;
        },
        set: function (callback) {
            this._onTrackMetadata = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(FLVDemuxer.prototype, "onMediaInfo", {
        // prototype: function(mediaInfo: MediaInfo): void
        get: function () {
            return this._onMediaInfo;
        },
        set: function (callback) {
            this._onMediaInfo = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(FLVDemuxer.prototype, "onMetaDataArrived", {
        get: function () {
            return this._onMetaDataArrived;
        },
        set: function (callback) {
            this._onMetaDataArrived = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(FLVDemuxer.prototype, "onScriptDataArrived", {
        get: function () {
            return this._onScriptDataArrived;
        },
        set: function (callback) {
            this._onScriptDataArrived = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(FLVDemuxer.prototype, "onError", {
        // prototype: function(type: number, info: string): void
        get: function () {
            return this._onError;
        },
        set: function (callback) {
            this._onError = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(FLVDemuxer.prototype, "onDataAvailable", {
        // prototype: function(videoTrack: any, audioTrack: any): void
        get: function () {
            return this._onDataAvailable;
        },
        set: function (callback) {
            this._onDataAvailable = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(FLVDemuxer.prototype, "timestampBase", {
        // timestamp base for output samples, must be in milliseconds
        get: function () {
            return this._timestampBase;
        },
        set: function (base) {
            this._timestampBase = base;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(FLVDemuxer.prototype, "overridedDuration", {
        get: function () {
            return this._duration;
        },
        // Force-override media duration. Must be in milliseconds, int32
        set: function (duration) {
            this._durationOverrided = true;
            this._duration = duration;
            this._mediaInfo.duration = duration;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(FLVDemuxer.prototype, "overridedHasAudio", {
        // Force-override audio track present flag, boolean
        set: function (hasAudio) {
            this._hasAudioFlagOverrided = true;
            this._hasAudio = hasAudio;
            this._mediaInfo.hasAudio = hasAudio;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(FLVDemuxer.prototype, "overridedHasVideo", {
        // Force-override video track present flag, boolean
        set: function (hasVideo) {
            this._hasVideoFlagOverrided = true;
            this._hasVideo = hasVideo;
            this._mediaInfo.hasVideo = hasVideo;
        },
        enumerable: false,
        configurable: true
    });
    FLVDemuxer.prototype.resetMediaInfo = function () {
        this._mediaInfo = new _core_media_info_js__WEBPACK_IMPORTED_MODULE_4__["default"]();
    };
    FLVDemuxer.prototype._isInitialMetadataDispatched = function () {
        if (this._hasAudio && this._hasVideo) { // both audio & video
            return this._audioInitialMetadataDispatched && this._videoInitialMetadataDispatched;
        }
        if (this._hasAudio && !this._hasVideo) { // audio only
            return this._audioInitialMetadataDispatched;
        }
        if (!this._hasAudio && this._hasVideo) { // video only
            return this._videoInitialMetadataDispatched;
        }
        return false;
    };
    // function parseChunks(chunk: ArrayBuffer, byteStart: number): number;
    FLVDemuxer.prototype.parseChunks = function (chunk, byteStart) {
        if (!this._onError || !this._onMediaInfo || !this._onTrackMetadata || !this._onDataAvailable) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_5__["IllegalStateException"]('Flv: onError & onMediaInfo & onTrackMetadata & onDataAvailable callback must be specified');
        }
        var offset = 0;
        var le = this._littleEndian;
        if (byteStart === 0) { // buffer with FLV header
            if (chunk.byteLength > 13) {
                var probeData = FLVDemuxer.probe(chunk);
                offset = probeData.dataOffset;
            }
            else {
                return 0;
            }
        }
        if (this._firstParse) { // handle PreviousTagSize0 before Tag1
            this._firstParse = false;
            if (byteStart + offset !== this._dataOffset) {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'First time parsing but chunk byteStart invalid!');
            }
            var v = new DataView(chunk, offset);
            var prevTagSize0 = v.getUint32(0, !le);
            if (prevTagSize0 !== 0) {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'PrevTagSize0 !== 0 !!!');
            }
            offset += 4;
        }
        while (offset < chunk.byteLength) {
            this._dispatch = true;
            var v = new DataView(chunk, offset);
            if (offset + 11 + 4 > chunk.byteLength) {
                // data not enough for parsing an flv tag
                break;
            }
            var tagType = v.getUint8(0);
            var dataSize = v.getUint32(0, !le) & 0x00FFFFFF;
            if (offset + 11 + dataSize + 4 > chunk.byteLength) {
                // data not enough for parsing actual data body
                break;
            }
            if (tagType !== 8 && tagType !== 9 && tagType !== 18) {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "Unsupported tag type ".concat(tagType, ", skipped"));
                // consume the whole tag (skip it)
                offset += 11 + dataSize + 4;
                continue;
            }
            var ts2 = v.getUint8(4);
            var ts1 = v.getUint8(5);
            var ts0 = v.getUint8(6);
            var ts3 = v.getUint8(7);
            var timestamp = ts0 | (ts1 << 8) | (ts2 << 16) | (ts3 << 24);
            var streamId = v.getUint32(7, !le) & 0x00FFFFFF;
            if (streamId !== 0) {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Meet tag which has StreamID != 0!');
            }
            var dataOffset = offset + 11;
            switch (tagType) {
                case 8: // Audio
                    this._parseAudioData(chunk, dataOffset, dataSize, timestamp);
                    break;
                case 9: // Video
                    this._parseVideoData(chunk, dataOffset, dataSize, timestamp, byteStart + offset);
                    break;
                case 18: // ScriptDataObject
                    this._parseScriptData(chunk, dataOffset, dataSize);
                    break;
            }
            var prevTagSize = v.getUint32(11 + dataSize, !le);
            if (prevTagSize !== 11 + dataSize) {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "Invalid PrevTagSize ".concat(prevTagSize));
            }
            offset += 11 + dataSize + 4; // tagBody + dataSize + prevTagSize
        }
        // dispatch parsed frames to consumer (typically, the remuxer)
        if (this._isInitialMetadataDispatched()) {
            if (this._dispatch && (this._audioTrack.length || this._videoTrack.length)) {
                this._onDataAvailable(this._audioTrack, this._videoTrack);
            }
        }
        return offset; // consumed bytes, just equals latest offset index
    };
    FLVDemuxer.prototype._parseScriptData = function (arrayBuffer, dataOffset, dataSize) {
        var scriptData = _amf_parser_js__WEBPACK_IMPORTED_MODULE_1__["default"].parseScriptData(arrayBuffer, dataOffset, dataSize);
        if (scriptData.hasOwnProperty('onMetaData')) {
            if (scriptData.onMetaData == null || typeof scriptData.onMetaData !== 'object') {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Invalid onMetaData structure!');
                return;
            }
            if (this._metadata) {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Found another onMetaData tag!');
            }
            this._metadata = scriptData;
            var onMetaData = this._metadata.onMetaData;
            if (this._onMetaDataArrived) {
                this._onMetaDataArrived(Object.assign({}, onMetaData));
            }
            if (typeof onMetaData.hasAudio === 'boolean') { // hasAudio
                if (this._hasAudioFlagOverrided === false) {
                    this._hasAudio = onMetaData.hasAudio;
                    this._mediaInfo.hasAudio = this._hasAudio;
                }
            }
            if (typeof onMetaData.hasVideo === 'boolean') { // hasVideo
                if (this._hasVideoFlagOverrided === false) {
                    this._hasVideo = onMetaData.hasVideo;
                    this._mediaInfo.hasVideo = this._hasVideo;
                }
            }
            if (typeof onMetaData.audiodatarate === 'number') { // audiodatarate
                this._mediaInfo.audioDataRate = onMetaData.audiodatarate;
            }
            if (typeof onMetaData.videodatarate === 'number') { // videodatarate
                this._mediaInfo.videoDataRate = onMetaData.videodatarate;
            }
            if (typeof onMetaData.width === 'number') { // width
                this._mediaInfo.width = onMetaData.width;
            }
            if (typeof onMetaData.height === 'number') { // height
                this._mediaInfo.height = onMetaData.height;
            }
            if (typeof onMetaData.duration === 'number') { // duration
                if (!this._durationOverrided) {
                    var duration = Math.floor(onMetaData.duration * this._timescale);
                    this._duration = duration;
                    this._mediaInfo.duration = duration;
                }
            }
            else {
                this._mediaInfo.duration = 0;
            }
            if (typeof onMetaData.framerate === 'number') { // framerate
                var fps_num = Math.floor(onMetaData.framerate * 1000);
                if (fps_num > 0) {
                    var fps = fps_num / 1000;
                    this._referenceFrameRate.fixed = true;
                    this._referenceFrameRate.fps = fps;
                    this._referenceFrameRate.fps_num = fps_num;
                    this._referenceFrameRate.fps_den = 1000;
                    this._mediaInfo.fps = fps;
                }
            }
            if (typeof onMetaData.keyframes === 'object') { // keyframes
                this._mediaInfo.hasKeyframesIndex = true;
                var keyframes = onMetaData.keyframes;
                this._mediaInfo.keyframesIndex = this._parseKeyframesIndex(keyframes);
                onMetaData.keyframes = null; // keyframes has been extracted, remove it
            }
            else {
                this._mediaInfo.hasKeyframesIndex = false;
            }
            this._dispatch = false;
            this._mediaInfo.metadata = onMetaData;
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Parsed onMetaData');
            if (this._mediaInfo.isComplete()) {
                this._onMediaInfo(this._mediaInfo);
            }
        }
        if (Object.keys(scriptData).length > 0) {
            if (this._onScriptDataArrived) {
                this._onScriptDataArrived(Object.assign({}, scriptData));
            }
        }
    };
    FLVDemuxer.prototype._parseKeyframesIndex = function (keyframes) {
        var times = [];
        var filepositions = [];
        // ignore first keyframe which is actually AVC/HEVC Sequence Header (AVCDecoderConfigurationRecord or HEVCDecoderConfigurationRecord)
        for (var i = 1; i < keyframes.times.length; i++) {
            var time = this._timestampBase + Math.floor(keyframes.times[i] * 1000);
            times.push(time);
            filepositions.push(keyframes.filepositions[i]);
        }
        return {
            times: times,
            filepositions: filepositions
        };
    };
    FLVDemuxer.prototype._parseAudioData = function (arrayBuffer, dataOffset, dataSize, tagTimestamp) {
        if (dataSize <= 1) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Flv: Invalid audio packet, missing SoundData payload!');
            return;
        }
        if (this._hasAudioFlagOverrided === true && this._hasAudio === false) {
            // If hasAudio: false indicated explicitly in MediaDataSource,
            // Ignore all the audio packets
            return;
        }
        var le = this._littleEndian;
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        var soundSpec = v.getUint8(0);
        var soundFormat = soundSpec >>> 4;
        if (soundFormat === 9) { // Enhanced FLV
            if (dataSize <= 5) {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Flv: Invalid audio packet, missing AudioFourCC in Ehnanced FLV payload!');
                return;
            }
            var packetType = soundSpec & 0x0F;
            var fourcc = String.fromCharCode.apply(String, (new Uint8Array(arrayBuffer, dataOffset, dataSize)).slice(1, 5));
            switch (fourcc) {
                case 'Opus':
                    this._parseOpusAudioPacket(arrayBuffer, dataOffset + 5, dataSize - 5, tagTimestamp, packetType);
                    break;
                case 'fLaC':
                    this._parseFlacAudioPacket(arrayBuffer, dataOffset + 5, dataSize - 5, tagTimestamp, packetType);
                    break;
                default:
                    this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].CODEC_UNSUPPORTED, 'Flv: Unsupported audio codec: ' + fourcc);
            }
            return;
        }
        // Legacy FLV
        if (soundFormat !== 2 && soundFormat !== 3 && soundFormat !== 10) { // PCM or MP3 or AAC
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].CODEC_UNSUPPORTED, 'Flv: Unsupported audio codec idx: ' + soundFormat);
            return;
        }
        var soundRate = 0;
        var soundRateIndex = (soundSpec & 12) >>> 2;
        if (soundRateIndex >= 0 && soundRateIndex <= 4) {
            soundRate = this._flvSoundRateTable[soundRateIndex];
        }
        else {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, 'Flv: Invalid audio sample rate idx: ' + soundRateIndex);
            return;
        }
        var soundSize = (soundSpec & 2) >>> 1; // unused
        var soundType = (soundSpec & 1);
        var meta = this._audioMetadata;
        var track = this._audioTrack;
        if (!meta) {
            if (this._hasAudio === false && this._hasAudioFlagOverrided === false) {
                this._hasAudio = true;
                this._mediaInfo.hasAudio = true;
            }
            // initial metadata
            meta = this._audioMetadata = {};
            meta.type = 'audio';
            meta.id = track.id;
            meta.timescale = this._timescale;
            meta.duration = this._duration;
            meta.audioSampleRate = soundRate;
            meta.channelCount = (soundType === 0 ? 1 : 2);
        }
        if (soundFormat === 10) { // AAC
            var aacData = this._parseAACAudioData(arrayBuffer, dataOffset + 1, dataSize - 1);
            if (aacData == undefined) {
                return;
            }
            if (aacData.packetType === 0) { // AAC sequence header (AudioSpecificConfig)
                if (meta.config) {
                    if (Object(_utils_typedarray_equality_ts__WEBPACK_IMPORTED_MODULE_7__["default"])(aacData.data.config, meta.config)) {
                        // If AudioSpecificConfig is not changed, ignore it to avoid generating initialization segment repeatedly
                        return;
                    }
                    else {
                        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'AudioSpecificConfig has been changed, re-generate initialization segment');
                    }
                }
                var misc = aacData.data;
                meta.audioSampleRate = misc.samplingRate;
                meta.channelCount = misc.channelCount;
                meta.codec = misc.codec;
                meta.originalCodec = misc.originalCodec;
                meta.config = misc.config;
                // The decode result of an aac sample is 1024 PCM samples
                meta.refSampleDuration = 1024 / meta.audioSampleRate * meta.timescale;
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Parsed AudioSpecificConfig');
                if (this._isInitialMetadataDispatched()) {
                    // Non-initial metadata, force dispatch (or flush) parsed frames to remuxer
                    if (this._dispatch && (this._audioTrack.length || this._videoTrack.length)) {
                        this._onDataAvailable(this._audioTrack, this._videoTrack);
                    }
                }
                else {
                    this._audioInitialMetadataDispatched = true;
                }
                // then notify new metadata
                this._dispatch = false;
                this._onTrackMetadata('audio', meta);
                var mi = this._mediaInfo;
                mi.audioCodec = meta.originalCodec;
                mi.audioSampleRate = meta.audioSampleRate;
                mi.audioChannelCount = meta.channelCount;
                if (mi.hasVideo) {
                    if (mi.videoCodec != null) {
                        mi.mimeType = 'video/x-flv; codecs="' + mi.videoCodec + ',' + mi.audioCodec + '"';
                    }
                }
                else {
                    mi.mimeType = 'video/x-flv; codecs="' + mi.audioCodec + '"';
                }
                if (mi.isComplete()) {
                    this._onMediaInfo(mi);
                }
            }
            else if (aacData.packetType === 1) { // AAC raw frame data
                var dts = this._timestampBase + tagTimestamp;
                var aacSample = { unit: aacData.data, length: aacData.data.byteLength, dts: dts, pts: dts };
                track.samples.push(aacSample);
                track.length += aacData.data.length;
            }
            else {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "Flv: Unsupported AAC data type ".concat(aacData.packetType));
            }
        }
        else if (soundFormat === 2) { // MP3
            if (!meta.codec) {
                // We need metadata for mp3 audio track, extract info from frame header
                var misc = this._parseMP3AudioData(arrayBuffer, dataOffset + 1, dataSize - 1, true);
                if (misc == undefined) {
                    return;
                }
                meta.audioSampleRate = misc.samplingRate;
                meta.channelCount = misc.channelCount;
                meta.codec = misc.codec;
                meta.originalCodec = misc.originalCodec;
                // The decode result of an mp3 sample is 1152 PCM samples
                meta.refSampleDuration = 1152 / meta.audioSampleRate * meta.timescale;
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Parsed MPEG Audio Frame Header');
                this._audioInitialMetadataDispatched = true;
                this._onTrackMetadata('audio', meta);
                var mi = this._mediaInfo;
                mi.audioCodec = meta.codec;
                mi.audioSampleRate = meta.audioSampleRate;
                mi.audioChannelCount = meta.channelCount;
                mi.audioDataRate = misc.bitRate;
                if (mi.hasVideo) {
                    if (mi.videoCodec != null) {
                        mi.mimeType = 'video/x-flv; codecs="' + mi.videoCodec + ',' + mi.audioCodec + '"';
                    }
                }
                else {
                    mi.mimeType = 'video/x-flv; codecs="' + mi.audioCodec + '"';
                }
                if (mi.isComplete()) {
                    this._onMediaInfo(mi);
                }
            }
            // This packet is always a valid audio packet, extract it
            var data = this._parseMP3AudioData(arrayBuffer, dataOffset + 1, dataSize - 1, false);
            if (data == undefined) {
                return;
            }
            var dts = this._timestampBase + tagTimestamp;
            var mp3Sample = { unit: data, length: data.byteLength, dts: dts, pts: dts };
            track.samples.push(mp3Sample);
            track.length += data.length;
        }
        else if (soundFormat === 3) {
            if (!meta.codec) {
                meta.audioSampleRate = soundRate;
                meta.sampleSize = (soundSize + 1) * 8;
                meta.littleEndian = true;
                meta.codec = 'ipcm';
                meta.originalCodec = 'ipcm';
                this._audioInitialMetadataDispatched = true;
                this._onTrackMetadata('audio', meta);
                var mi = this._mediaInfo;
                mi.audioCodec = meta.codec;
                mi.audioSampleRate = meta.audioSampleRate;
                mi.audioChannelCount = meta.channelCount;
                mi.audioDataRate = meta.sampleSize * meta.audioSampleRate;
                if (mi.hasVideo) {
                    if (mi.videoCodec != null) {
                        mi.mimeType = 'video/x-flv; codecs="' + mi.videoCodec + ',' + mi.audioCodec + '"';
                    }
                }
                else {
                    mi.mimeType = 'video/x-flv; codecs="' + mi.audioCodec + '"';
                }
                if (mi.isComplete()) {
                    this._onMediaInfo(mi);
                }
            }
            var data = new Uint8Array(arrayBuffer, dataOffset + 1, dataSize - 1);
            var dts = this._timestampBase + tagTimestamp;
            var pcmSample = { unit: data, length: data.byteLength, dts: dts, pts: dts };
            track.samples.push(pcmSample);
            track.length += data.length;
        }
    };
    FLVDemuxer.prototype._parseAACAudioData = function (arrayBuffer, dataOffset, dataSize) {
        if (dataSize <= 1) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Flv: Invalid AAC packet, missing AACPacketType or/and Data!');
            return;
        }
        var result = {};
        var array = new Uint8Array(arrayBuffer, dataOffset, dataSize);
        result.packetType = array[0];
        if (array[0] === 0) {
            result.data = this._parseAACAudioSpecificConfig(arrayBuffer, dataOffset + 1, dataSize - 1);
        }
        else {
            result.data = array.subarray(1);
        }
        return result;
    };
    FLVDemuxer.prototype._parseAACAudioSpecificConfig = function (arrayBuffer, dataOffset, dataSize) {
        var array = new Uint8Array(arrayBuffer, dataOffset, dataSize);
        var config = null;
        /* Audio Object Type:
           0: Null
           1: AAC Main
           2: AAC LC
           3: AAC SSR (Scalable Sample Rate)
           4: AAC LTP (Long Term Prediction)
           5: HE-AAC / SBR (Spectral Band Replication)
           6: AAC Scalable
        */
        var audioObjectType = 0;
        var originalAudioObjectType = 0;
        var audioExtensionObjectType = null;
        var samplingIndex = 0;
        var extensionSamplingIndex = null;
        // 5 bits
        audioObjectType = originalAudioObjectType = array[0] >>> 3;
        // 4 bits
        samplingIndex = ((array[0] & 0x07) << 1) | (array[1] >>> 7);
        if (samplingIndex < 0 || samplingIndex >= this._mpegSamplingRates.length) {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, 'Flv: AAC invalid sampling frequency index!');
            return;
        }
        var samplingFrequence = this._mpegSamplingRates[samplingIndex];
        // 4 bits
        var channelConfig = (array[1] & 0x78) >>> 3;
        if (channelConfig < 0 || channelConfig >= 8) {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, 'Flv: AAC invalid channel configuration');
            return;
        }
        if (audioObjectType === 5) { // HE-AAC?
            // 4 bits
            extensionSamplingIndex = ((array[1] & 0x07) << 1) | (array[2] >>> 7);
            // 5 bits
            audioExtensionObjectType = (array[2] & 0x7C) >>> 2;
        }
        // workarounds for various browsers
        var userAgent = self.navigator.userAgent.toLowerCase();
        if (userAgent.indexOf('firefox') !== -1) {
            // firefox: use SBR (HE-AAC) if freq less than 24kHz
            if (samplingIndex >= 6) {
                audioObjectType = 5;
                config = new Array(4);
                extensionSamplingIndex = samplingIndex - 3;
            }
            else { // use LC-AAC
                audioObjectType = 2;
                config = new Array(2);
                extensionSamplingIndex = samplingIndex;
            }
        }
        else if (userAgent.indexOf('android') !== -1) {
            // android: always use LC-AAC
            audioObjectType = 2;
            config = new Array(2);
            extensionSamplingIndex = samplingIndex;
        }
        else {
            // for other browsers, e.g. chrome...
            // Always use HE-AAC to make it easier to switch aac codec profile
            audioObjectType = 5;
            extensionSamplingIndex = samplingIndex;
            config = new Array(4);
            if (samplingIndex >= 6) {
                extensionSamplingIndex = samplingIndex - 3;
            }
            else if (channelConfig === 1) { // Mono channel
                audioObjectType = 2;
                config = new Array(2);
                extensionSamplingIndex = samplingIndex;
            }
        }
        config[0] = audioObjectType << 3;
        config[0] |= (samplingIndex & 0x0F) >>> 1;
        config[1] = (samplingIndex & 0x0F) << 7;
        config[1] |= (channelConfig & 0x0F) << 3;
        if (audioObjectType === 5) {
            config[1] |= ((extensionSamplingIndex & 0x0F) >>> 1);
            config[2] = (extensionSamplingIndex & 0x01) << 7;
            // extended audio object type: force to 2 (LC-AAC)
            config[2] |= (2 << 2);
            config[3] = 0;
        }
        return {
            config: config,
            samplingRate: samplingFrequence,
            channelCount: channelConfig,
            codec: 'mp4a.40.' + audioObjectType,
            originalCodec: 'mp4a.40.' + originalAudioObjectType
        };
    };
    FLVDemuxer.prototype._parseMP3AudioData = function (arrayBuffer, dataOffset, dataSize, requestHeader) {
        if (dataSize < 4) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Flv: Invalid MP3 packet, header missing!');
            return;
        }
        var le = this._littleEndian;
        var array = new Uint8Array(arrayBuffer, dataOffset, dataSize);
        var result = null;
        if (requestHeader) {
            if (array[0] !== 0xFF) {
                return;
            }
            var ver = (array[1] >>> 3) & 0x03;
            var layer = (array[1] & 0x06) >> 1;
            var bitrate_index = (array[2] & 0xF0) >>> 4;
            var sampling_freq_index = (array[2] & 0x0C) >>> 2;
            var channel_mode = (array[3] >>> 6) & 0x03;
            var channel_count = channel_mode !== 3 ? 2 : 1;
            var sample_rate = 0;
            var bit_rate = 0;
            var object_type = 34; // Layer-3, listed in MPEG-4 Audio Object Types
            var codec = 'mp3';
            switch (ver) {
                case 0: // MPEG 2.5
                    sample_rate = this._mpegAudioV25SampleRateTable[sampling_freq_index];
                    break;
                case 2: // MPEG 2
                    sample_rate = this._mpegAudioV20SampleRateTable[sampling_freq_index];
                    break;
                case 3: // MPEG 1
                    sample_rate = this._mpegAudioV10SampleRateTable[sampling_freq_index];
                    break;
            }
            switch (layer) {
                case 1: // Layer 3
                    object_type = 34;
                    if (bitrate_index < this._mpegAudioL3BitRateTable.length) {
                        bit_rate = this._mpegAudioL3BitRateTable[bitrate_index];
                    }
                    break;
                case 2: // Layer 2
                    object_type = 33;
                    if (bitrate_index < this._mpegAudioL2BitRateTable.length) {
                        bit_rate = this._mpegAudioL2BitRateTable[bitrate_index];
                    }
                    break;
                case 3: // Layer 1
                    object_type = 32;
                    if (bitrate_index < this._mpegAudioL1BitRateTable.length) {
                        bit_rate = this._mpegAudioL1BitRateTable[bitrate_index];
                    }
                    break;
            }
            result = {
                bitRate: bit_rate,
                samplingRate: sample_rate,
                channelCount: channel_count,
                codec: codec,
                originalCodec: codec
            };
        }
        else {
            result = array;
        }
        return result;
    };
    FLVDemuxer.prototype._parseOpusAudioPacket = function (arrayBuffer, dataOffset, dataSize, tagTimestamp, packetType) {
        if (packetType === 0) { // OpusSequenceHeader
            this._parseOpusSequenceHeader(arrayBuffer, dataOffset, dataSize);
        }
        else if (packetType === 1) { // OpusCodedData
            this._parseOpusAudioData(arrayBuffer, dataOffset, dataSize, tagTimestamp);
        }
        else if (packetType === 2) {
            // empty, Opus end of sequence
        }
        else {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, "Flv: Invalid video packet type ".concat(packetType));
            return;
        }
    };
    FLVDemuxer.prototype._parseOpusSequenceHeader = function (arrayBuffer, dataOffset, dataSize) {
        if (dataSize <= 16) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Flv: Invalid OpusSequenceHeader, lack of data!');
            return;
        }
        var meta = this._audioMetadata;
        var track = this._audioTrack;
        if (!meta) {
            if (this._hasAudio === false && this._hasAudioFlagOverrided === false) {
                this._hasAudio = true;
                this._mediaInfo.hasAudio = true;
            }
            // initial metadata
            meta = this._audioMetadata = {};
            meta.type = 'audio';
            meta.id = track.id;
            meta.timescale = this._timescale;
            meta.duration = this._duration;
        }
        // Identification Header
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        v.setUint8(8 + 0, 0); // set version to 0
        var channelCount = v.getUint8(8 + 1); // Opus Header + 1
        v.setUint16(8 + 2, v.getUint16(8 + 2, true), false); // Big Endian to Little Endian for Pre-skip
        var samplingFrequence = v.getUint32(8 + 4, true); // Opus Header + 4
        v.setUint32(8 + 4, v.getUint32(8 + 4, true), false); // Big Endian to Little Endian for Input Sample Rate
        var config = new Uint8Array(arrayBuffer, dataOffset + 8, dataSize - 8);
        var misc = {
            config: config,
            channelCount: channelCount,
            samplingFrequence: samplingFrequence,
            codec: 'opus',
            originalCodec: 'opus',
        };
        if (meta.config) {
            if (Object(_utils_typedarray_equality_ts__WEBPACK_IMPORTED_MODULE_7__["default"])(misc.config, meta.config)) {
                // If OpusSequenceHeader is not changed, ignore it to avoid generating initialization segment repeatedly
                return;
            }
            else {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'OpusSequenceHeader has been changed, re-generate initialization segment');
            }
        }
        meta.audioSampleRate = misc.samplingFrequence;
        meta.channelCount = misc.channelCount;
        meta.codec = misc.codec;
        meta.originalCodec = misc.originalCodec;
        meta.config = misc.config;
        // The decode result of an opus sample is 20ms
        meta.refSampleDuration = 20;
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Parsed OpusSequenceHeader');
        if (this._isInitialMetadataDispatched()) {
            // Non-initial metadata, force dispatch (or flush) parsed frames to remuxer
            if (this._dispatch && (this._audioTrack.length || this._videoTrack.length)) {
                this._onDataAvailable(this._audioTrack, this._videoTrack);
            }
        }
        else {
            this._audioInitialMetadataDispatched = true;
        }
        // then notify new metadata
        this._dispatch = false;
        this._onTrackMetadata('audio', meta);
        var mi = this._mediaInfo;
        mi.audioCodec = meta.originalCodec;
        mi.audioSampleRate = meta.audioSampleRate;
        mi.audioChannelCount = meta.channelCount;
        if (mi.hasVideo) {
            if (mi.videoCodec != null) {
                mi.mimeType = 'video/x-flv; codecs="' + mi.videoCodec + ',' + mi.audioCodec + '"';
            }
        }
        else {
            mi.mimeType = 'video/x-flv; codecs="' + mi.audioCodec + '"';
        }
        if (mi.isComplete()) {
            this._onMediaInfo(mi);
        }
    };
    FLVDemuxer.prototype._parseOpusAudioData = function (arrayBuffer, dataOffset, dataSize, tagTimestamp) {
        var track = this._audioTrack;
        var data = new Uint8Array(arrayBuffer, dataOffset, dataSize);
        var dts = this._timestampBase + tagTimestamp;
        var opusSample = { unit: data, length: data.byteLength, dts: dts, pts: dts };
        track.samples.push(opusSample);
        track.length += data.length;
    };
    FLVDemuxer.prototype._parseFlacAudioPacket = function (arrayBuffer, dataOffset, dataSize, tagTimestamp, packetType) {
        if (packetType === 0) { // FlacSequenceHeader
            this._parseFlacSequenceHeader(arrayBuffer, dataOffset, dataSize);
        }
        else if (packetType === 1) { // FlacCodedData
            this._parseFlacAudioData(arrayBuffer, dataOffset, dataSize, tagTimestamp);
        }
        else if (packetType === 2) {
            // empty, Flac end of sequence
        }
        else {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, "Flv: Invalid Flac audio packet type ".concat(packetType));
            return;
        }
    };
    FLVDemuxer.prototype._parseFlacSequenceHeader = function (arrayBuffer, dataOffset, dataSize) {
        var meta = this._audioMetadata;
        var track = this._audioTrack;
        if (!meta) {
            if (this._hasAudio === false && this._hasAudioFlagOverrided === false) {
                this._hasAudio = true;
                this._mediaInfo.hasAudio = true;
            }
            // initial metadata
            meta = this._audioMetadata = {};
            meta.type = 'audio';
            meta.id = track.id;
            meta.timescale = this._timescale;
            meta.duration = this._duration;
        }
        // METADATA_BLOCK_HEADER
        var header = new Uint8Array(arrayBuffer, dataOffset + 4, dataSize - 4);
        var gb = new _exp_golomb_js__WEBPACK_IMPORTED_MODULE_9__["default"](header);
        var minimum_block_size = gb.readBits(16); // minimum_block_size
        var maximum_block_size = gb.readBits(16); // maximum_block_size
        var block_size = maximum_block_size === minimum_block_size ? maximum_block_size : null;
        gb.readBits(24); // minimum_frame_size
        gb.readBits(24); // maximum_frame_size
        var samplingFrequence = gb.readBits(20);
        var channelCount = gb.readBits(3) + 1;
        var sampleSize = gb.readBits(5) + 1;
        gb.destroy();
        var config = new Uint8Array(header.byteLength + 4);
        config.set(header, 4);
        config[0] = 1 << 7;
        config[1] = (header.byteLength >>> 16) & 0xFF;
        config[2] = (header.byteLength >>> 8) & 0xFF;
        config[3] = (header.byteLength >>> 0) & 0xFF;
        var misc = {
            config: config,
            channelCount: channelCount,
            samplingFrequence: samplingFrequence,
            sampleSize: sampleSize,
            codec: 'flac',
            originalCodec: 'flac',
        };
        if (meta.config) {
            if (Object(_utils_typedarray_equality_ts__WEBPACK_IMPORTED_MODULE_7__["default"])(misc.config, meta.config)) {
                // If FlacSequenceHeader is not changed, ignore it to avoid generating initialization segment repeatedly
                return;
            }
            else {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'FlacSequenceHeader has been changed, re-generate initialization segment');
            }
        }
        meta.audioSampleRate = misc.samplingFrequence;
        meta.channelCount = misc.channelCount;
        meta.sampleSize = misc.sampleSize;
        meta.codec = misc.codec;
        meta.originalCodec = misc.originalCodec;
        meta.config = misc.config;
        meta.refSampleDuration = block_size != null ? block_size * 1000 / misc.samplingFrequence : null; // practical encoder sends 4608 blobksize (lower bound limitation)
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Parsed FlacSequenceHeader');
        if (this._isInitialMetadataDispatched()) {
            // Non-initial metadata, force dispatch (or flush) parsed frames to remuxer
            if (this._dispatch && (this._audioTrack.length || this._videoTrack.length)) {
                this._onDataAvailable(this._audioTrack, this._videoTrack);
            }
        }
        else {
            this._audioInitialMetadataDispatched = true;
        }
        // then notify new metadata
        this._dispatch = false;
        this._onTrackMetadata('audio', meta);
        var mi = this._mediaInfo;
        mi.audioCodec = meta.originalCodec;
        mi.audioSampleRate = meta.audioSampleRate;
        mi.audioChannelCount = meta.channelCount;
        if (mi.hasVideo) {
            if (mi.videoCodec != null) {
                mi.mimeType = 'video/x-flv; codecs="' + mi.videoCodec + ',' + mi.audioCodec + '"';
            }
        }
        else {
            mi.mimeType = 'video/x-flv; codecs="' + mi.audioCodec + '"';
        }
        if (mi.isComplete()) {
            this._onMediaInfo(mi);
        }
    };
    FLVDemuxer.prototype._parseFlacAudioData = function (arrayBuffer, dataOffset, dataSize, tagTimestamp) {
        var track = this._audioTrack;
        var data = new Uint8Array(arrayBuffer, dataOffset, dataSize);
        var dts = this._timestampBase + tagTimestamp;
        var flacSample = { unit: data, length: data.byteLength, dts: dts, pts: dts };
        track.samples.push(flacSample);
        track.length += data.length;
    };
    FLVDemuxer.prototype._parseVideoData = function (arrayBuffer, dataOffset, dataSize, tagTimestamp, tagPosition) {
        if (dataSize <= 1) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Flv: Invalid video packet, missing VideoData payload!');
            return;
        }
        if (this._hasVideoFlagOverrided === true && this._hasVideo === false) {
            // If hasVideo: false indicated explicitly in MediaDataSource,
            // Ignore all the video packets
            return;
        }
        var spec = (new Uint8Array(arrayBuffer, dataOffset, dataSize))[0];
        var isExHeader = (spec & 128) !== 0;
        var frameType = (spec & 112) >>> 4;
        if (!isExHeader) {
            var codecId = spec & 15;
            if (codecId === 7) { // AVC
                this._parseAVCVideoPacket(arrayBuffer, dataOffset + 1, dataSize - 1, tagTimestamp, tagPosition, frameType);
            }
            else if (codecId === 12) { // HEVC
                this._parseHEVCVideoPacket(arrayBuffer, dataOffset + 1, dataSize - 1, tagTimestamp, tagPosition, frameType);
            }
            else {
                this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].CODEC_UNSUPPORTED, "Flv: Unsupported codec in video frame: ".concat(codecId));
                return;
            }
        }
        else {
            var packetType = spec & 15;
            var fourcc = String.fromCharCode.apply(String, (new Uint8Array(arrayBuffer, dataOffset, dataSize)).slice(1, 5));
            if (fourcc === 'hvc1') { // HEVC
                this._parseEnhancedHEVCVideoPacket(arrayBuffer, dataOffset + 5, dataSize - 5, tagTimestamp, tagPosition, frameType, packetType);
            }
            else if (fourcc === 'av01') { // HEVC
                this._parseEnhancedAV1VideoPacket(arrayBuffer, dataOffset + 5, dataSize - 5, tagTimestamp, tagPosition, frameType, packetType);
            }
            else {
                this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].CODEC_UNSUPPORTED, "Flv: Unsupported codec in video frame: ".concat(fourcc));
                return;
            }
        }
    };
    FLVDemuxer.prototype._parseAVCVideoPacket = function (arrayBuffer, dataOffset, dataSize, tagTimestamp, tagPosition, frameType) {
        if (dataSize < 4) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Flv: Invalid AVC packet, missing AVCPacketType or/and CompositionTime');
            return;
        }
        var le = this._littleEndian;
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        var packetType = v.getUint8(0);
        var cts_unsigned = v.getUint32(0, !le) & 0x00FFFFFF;
        var cts = (cts_unsigned << 8) >> 8; // convert to 24-bit signed int
        if (packetType === 0) { // AVCDecoderConfigurationRecord
            this._parseAVCDecoderConfigurationRecord(arrayBuffer, dataOffset + 4, dataSize - 4);
        }
        else if (packetType === 1) { // One or more Nalus
            this._parseAVCVideoData(arrayBuffer, dataOffset + 4, dataSize - 4, tagTimestamp, tagPosition, frameType, cts);
        }
        else if (packetType === 2) {
            // empty, AVC end of sequence
        }
        else {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, "Flv: Invalid video packet type ".concat(packetType));
            return;
        }
    };
    FLVDemuxer.prototype._parseHEVCVideoPacket = function (arrayBuffer, dataOffset, dataSize, tagTimestamp, tagPosition, frameType) {
        if (dataSize < 4) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Flv: Invalid HEVC packet, missing HEVCPacketType or/and CompositionTime');
            return;
        }
        var le = this._littleEndian;
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        var packetType = v.getUint8(0);
        var cts_unsigned = v.getUint32(0, !le) & 0x00FFFFFF;
        var cts = (cts_unsigned << 8) >> 8; // convert to 24-bit signed int
        if (packetType === 0) { // HEVCDecoderConfigurationRecord
            this._parseHEVCDecoderConfigurationRecord(arrayBuffer, dataOffset + 4, dataSize - 4);
        }
        else if (packetType === 1) { // One or more Nalus
            this._parseHEVCVideoData(arrayBuffer, dataOffset + 4, dataSize - 4, tagTimestamp, tagPosition, frameType, cts);
        }
        else if (packetType === 2) {
            // empty, HEVC end of sequence
        }
        else {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, "Flv: Invalid video packet type ".concat(packetType));
            return;
        }
    };
    FLVDemuxer.prototype._parseEnhancedHEVCVideoPacket = function (arrayBuffer, dataOffset, dataSize, tagTimestamp, tagPosition, frameType, packetType) {
        var le = this._littleEndian;
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        if (packetType === 0) { // HEVCDecoderConfigurationRecord
            this._parseHEVCDecoderConfigurationRecord(arrayBuffer, dataOffset, dataSize);
        }
        else if (packetType === 1) { // One or more Nalus
            var cts_unsigned = v.getUint32(0, !le) & 0xFFFFFF00;
            var cts = cts_unsigned >> 8; // convert to 24-bit signed int
            this._parseHEVCVideoData(arrayBuffer, dataOffset + 3, dataSize - 3, tagTimestamp, tagPosition, frameType, cts);
        }
        else if (packetType === 3) {
            this._parseHEVCVideoData(arrayBuffer, dataOffset, dataSize, tagTimestamp, tagPosition, frameType, 0);
        }
        else if (packetType === 2) {
            // empty, HEVC end of sequence
        }
        else {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, "Flv: Invalid video packet type ".concat(packetType));
            return;
        }
    };
    FLVDemuxer.prototype._parseEnhancedAV1VideoPacket = function (arrayBuffer, dataOffset, dataSize, tagTimestamp, tagPosition, frameType, packetType) {
        var le = this._littleEndian;
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        if (packetType === 0) { // AV1CodecConfigurationRecord
            this._parseAV1CodecConfigurationRecord(arrayBuffer, dataOffset, dataSize);
        }
        else if (packetType === 1) { // One or more OBUs
            this._parseAV1VideoData(arrayBuffer, dataOffset, dataSize, tagTimestamp, tagPosition, frameType, 0);
        }
        else if (packetType === 5) {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, "Flv: Not Supported MP2T AV1 video packet type ".concat(packetType));
            return;
        }
        else if (packetType === 2) {
            // empty, AV1 end of sequence
        }
        else {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, "Flv: Invalid video packet type ".concat(packetType));
            return;
        }
    };
    FLVDemuxer.prototype._parseAVCDecoderConfigurationRecord = function (arrayBuffer, dataOffset, dataSize) {
        if (dataSize < 7) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Flv: Invalid AVCDecoderConfigurationRecord, lack of data!');
            return;
        }
        var meta = this._videoMetadata;
        var track = this._videoTrack;
        var le = this._littleEndian;
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        if (!meta) {
            if (this._hasVideo === false && this._hasVideoFlagOverrided === false) {
                this._hasVideo = true;
                this._mediaInfo.hasVideo = true;
            }
            meta = this._videoMetadata = {};
            meta.type = 'video';
            meta.id = track.id;
            meta.timescale = this._timescale;
            meta.duration = this._duration;
        }
        else {
            if (typeof meta.avcc !== 'undefined') {
                var new_avcc = new Uint8Array(arrayBuffer, dataOffset, dataSize);
                if (Object(_utils_typedarray_equality_ts__WEBPACK_IMPORTED_MODULE_7__["default"])(new_avcc, meta.avcc)) {
                    // AVCDecoderConfigurationRecord is not changed, ignore it to avoid initialization segment re-generating
                    return;
                }
                else {
                    _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'AVCDecoderConfigurationRecord has been changed, re-generate initialization segment');
                }
            }
        }
        var version = v.getUint8(0); // configurationVersion
        var avcProfile = v.getUint8(1); // avcProfileIndication
        var profileCompatibility = v.getUint8(2); // profile_compatibility
        var avcLevel = v.getUint8(3); // AVCLevelIndication
        if (version !== 1 || avcProfile === 0) {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, 'Flv: Invalid AVCDecoderConfigurationRecord');
            return;
        }
        this._naluLengthSize = (v.getUint8(4) & 3) + 1; // lengthSizeMinusOne
        if (this._naluLengthSize !== 3 && this._naluLengthSize !== 4) { // holy shit!!!
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, "Flv: Strange NaluLengthSizeMinusOne: ".concat(this._naluLengthSize - 1));
            return;
        }
        var spsCount = v.getUint8(5) & 31; // numOfSequenceParameterSets
        if (spsCount === 0) {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, 'Flv: Invalid AVCDecoderConfigurationRecord: No SPS');
            return;
        }
        else if (spsCount > 1) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "Flv: Strange AVCDecoderConfigurationRecord: SPS Count = ".concat(spsCount));
        }
        var offset = 6;
        for (var i = 0; i < spsCount; i++) {
            var len = v.getUint16(offset, !le); // sequenceParameterSetLength
            offset += 2;
            if (len === 0) {
                continue;
            }
            // Notice: Nalu without startcode header (00 00 00 01)
            var sps = new Uint8Array(arrayBuffer, dataOffset + offset, len);
            offset += len;
            var config = _sps_parser_js__WEBPACK_IMPORTED_MODULE_2__["default"].parseSPS(sps);
            if (i !== 0) {
                // ignore other sps's config
                continue;
            }
            meta.codecWidth = config.codec_size.width;
            meta.codecHeight = config.codec_size.height;
            meta.presentWidth = config.present_size.width;
            meta.presentHeight = config.present_size.height;
            meta.profile = config.profile_string;
            meta.level = config.level_string;
            meta.bitDepth = config.bit_depth;
            meta.chromaFormat = config.chroma_format;
            meta.sarRatio = config.sar_ratio;
            meta.frameRate = config.frame_rate;
            if (config.frame_rate.fixed === false ||
                config.frame_rate.fps_num === 0 ||
                config.frame_rate.fps_den === 0) {
                meta.frameRate = this._referenceFrameRate;
            }
            var fps_den = meta.frameRate.fps_den;
            var fps_num = meta.frameRate.fps_num;
            meta.refSampleDuration = meta.timescale * (fps_den / fps_num);
            var codecArray = sps.subarray(1, 4);
            var codecString = 'avc1.';
            for (var j = 0; j < 3; j++) {
                var h = codecArray[j].toString(16);
                if (h.length < 2) {
                    h = '0' + h;
                }
                codecString += h;
            }
            meta.codec = codecString;
            var mi = this._mediaInfo;
            mi.width = meta.codecWidth;
            mi.height = meta.codecHeight;
            mi.fps = meta.frameRate.fps;
            mi.profile = meta.profile;
            mi.level = meta.level;
            mi.refFrames = config.ref_frames;
            mi.chromaFormat = config.chroma_format_string;
            mi.sarNum = meta.sarRatio.width;
            mi.sarDen = meta.sarRatio.height;
            mi.videoCodec = codecString;
            if (mi.hasAudio) {
                if (mi.audioCodec != null) {
                    mi.mimeType = 'video/x-flv; codecs="' + mi.videoCodec + ',' + mi.audioCodec + '"';
                }
            }
            else {
                mi.mimeType = 'video/x-flv; codecs="' + mi.videoCodec + '"';
            }
            if (mi.isComplete()) {
                this._onMediaInfo(mi);
            }
        }
        var ppsCount = v.getUint8(offset); // numOfPictureParameterSets
        if (ppsCount === 0) {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, 'Flv: Invalid AVCDecoderConfigurationRecord: No PPS');
            return;
        }
        else if (ppsCount > 1) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "Flv: Strange AVCDecoderConfigurationRecord: PPS Count = ".concat(ppsCount));
        }
        offset++;
        for (var i = 0; i < ppsCount; i++) {
            var len = v.getUint16(offset, !le); // pictureParameterSetLength
            offset += 2;
            if (len === 0) {
                continue;
            }
            // pps is useless for extracting video information
            offset += len;
        }
        meta.avcc = new Uint8Array(dataSize);
        meta.avcc.set(new Uint8Array(arrayBuffer, dataOffset, dataSize), 0);
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Parsed AVCDecoderConfigurationRecord');
        if (this._isInitialMetadataDispatched()) {
            // flush parsed frames
            if (this._dispatch && (this._audioTrack.length || this._videoTrack.length)) {
                this._onDataAvailable(this._audioTrack, this._videoTrack);
            }
        }
        else {
            this._videoInitialMetadataDispatched = true;
        }
        // notify new metadata
        this._dispatch = false;
        this._onTrackMetadata('video', meta);
    };
    FLVDemuxer.prototype._parseHEVCDecoderConfigurationRecord = function (arrayBuffer, dataOffset, dataSize) {
        if (dataSize < 22) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Flv: Invalid HEVCDecoderConfigurationRecord, lack of data!');
            return;
        }
        var meta = this._videoMetadata;
        var track = this._videoTrack;
        var le = this._littleEndian;
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        if (!meta) {
            if (this._hasVideo === false && this._hasVideoFlagOverrided === false) {
                this._hasVideo = true;
                this._mediaInfo.hasVideo = true;
            }
            meta = this._videoMetadata = {};
            meta.type = 'video';
            meta.id = track.id;
            meta.timescale = this._timescale;
            meta.duration = this._duration;
        }
        else {
            if (typeof meta.hvcc !== 'undefined') {
                var new_hvcc = new Uint8Array(arrayBuffer, dataOffset, dataSize);
                if (Object(_utils_typedarray_equality_ts__WEBPACK_IMPORTED_MODULE_7__["default"])(new_hvcc, meta.hvcc)) {
                    // HEVCDecoderConfigurationRecord not changed, ignore it to avoid initialization segment re-generating
                    return;
                }
                else {
                    _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'HEVCDecoderConfigurationRecord has been changed, re-generate initialization segment');
                }
            }
        }
        var version = v.getUint8(0); // configurationVersion
        var hevcProfile = v.getUint8(1) & 0x1F; // hevcProfileIndication
        if ((version !== 0 && version !== 1) || hevcProfile === 0) {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, 'Flv: Invalid HEVCDecoderConfigurationRecord');
            return;
        }
        this._naluLengthSize = (v.getUint8(21) & 3) + 1; // lengthSizeMinusOne
        if (this._naluLengthSize !== 3 && this._naluLengthSize !== 4) { // holy shit!!!
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, "Flv: Strange NaluLengthSizeMinusOne: ".concat(this._naluLengthSize - 1));
            return;
        }
        var numOfArrays = v.getUint8(22);
        for (var i = 0, offset = 23; i < numOfArrays; i++) {
            var nalUnitType = v.getUint8(offset + 0) & 0x3F;
            var numNalus = v.getUint16(offset + 1, !le);
            offset += 3;
            for (var j = 0; j < numNalus; j++) {
                var len = v.getUint16(offset + 0, !le);
                if (j !== 0) {
                    offset += 2 + len;
                    continue;
                }
                if (nalUnitType === 33) {
                    offset += 2;
                    var sps = new Uint8Array(arrayBuffer, dataOffset + offset, len);
                    var config = _h265_parser_js__WEBPACK_IMPORTED_MODULE_6__["default"].parseSPS(sps);
                    meta.codecWidth = config.codec_size.width;
                    meta.codecHeight = config.codec_size.height;
                    meta.presentWidth = config.present_size.width;
                    meta.presentHeight = config.present_size.height;
                    meta.profile = config.profile_string;
                    meta.level = config.level_string;
                    meta.bitDepth = config.bit_depth;
                    meta.chromaFormat = config.chroma_format;
                    meta.sarRatio = config.sar_ratio;
                    meta.frameRate = config.frame_rate;
                    if (config.frame_rate.fixed === false ||
                        config.frame_rate.fps_num === 0 ||
                        config.frame_rate.fps_den === 0) {
                        meta.frameRate = this._referenceFrameRate;
                    }
                    var fps_den = meta.frameRate.fps_den;
                    var fps_num = meta.frameRate.fps_num;
                    meta.refSampleDuration = meta.timescale * (fps_den / fps_num);
                    meta.codec = config.codec_mimetype;
                    var mi = this._mediaInfo;
                    mi.width = meta.codecWidth;
                    mi.height = meta.codecHeight;
                    mi.fps = meta.frameRate.fps;
                    mi.profile = meta.profile;
                    mi.level = meta.level;
                    mi.refFrames = config.ref_frames;
                    mi.chromaFormat = config.chroma_format_string;
                    mi.sarNum = meta.sarRatio.width;
                    mi.sarDen = meta.sarRatio.height;
                    mi.videoCodec = config.codec_mimetype;
                    if (mi.hasAudio) {
                        if (mi.audioCodec != null) {
                            mi.mimeType = 'video/x-flv; codecs="' + mi.videoCodec + ',' + mi.audioCodec + '"';
                        }
                    }
                    else {
                        mi.mimeType = 'video/x-flv; codecs="' + mi.videoCodec + '"';
                    }
                    if (mi.isComplete()) {
                        this._onMediaInfo(mi);
                    }
                    offset += len;
                }
                else {
                    offset += 2 + len;
                }
            }
        }
        meta.hvcc = new Uint8Array(dataSize);
        meta.hvcc.set(new Uint8Array(arrayBuffer, dataOffset, dataSize), 0);
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Parsed HEVCDecoderConfigurationRecord');
        if (this._isInitialMetadataDispatched()) {
            // flush parsed frames
            if (this._dispatch && (this._audioTrack.length || this._videoTrack.length)) {
                this._onDataAvailable(this._audioTrack, this._videoTrack);
            }
        }
        else {
            this._videoInitialMetadataDispatched = true;
        }
        // notify new metadata
        this._dispatch = false;
        this._onTrackMetadata('video', meta);
    };
    FLVDemuxer.prototype._parseAV1CodecConfigurationRecord = function (arrayBuffer, dataOffset, dataSize) {
        if (dataSize < 4) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Flv: Invalid AV1CodecConfigurationRecord, lack of data!');
            return;
        }
        var meta = this._videoMetadata;
        var track = this._videoTrack;
        var le = this._littleEndian;
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        if (!meta) {
            if (this._hasVideo === false && this._hasVideoFlagOverrided === false) {
                this._hasVideo = true;
                this._mediaInfo.hasVideo = true;
            }
            meta = this._videoMetadata = {};
            meta.type = 'video';
            meta.id = track.id;
            meta.timescale = this._timescale;
            meta.duration = this._duration;
        }
        else {
            if (typeof meta.av1c !== 'undefined') {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Found another AV1CodecConfigurationRecord!');
            }
        }
        var version = v.getUint8(0) & 0x7F;
        var seq_profile = (v.getUint8(1) & 0xE0) >> 5;
        var seq_level_idx = (v.getUint8(1) & 0x8F) >> 0;
        var seq_tier = (v.getUint8(2) & 0x80) >> 7;
        if (version !== 1) {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, 'Flv: Invalid AV1CodecConfigurationRecord');
            return;
        }
        var config = _av1_parser_ts__WEBPACK_IMPORTED_MODULE_8__["default"].parseOBUs(new Uint8Array(arrayBuffer, dataOffset + 4, dataSize - 4));
        if (config == null) {
            this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, 'Flv: Invalid AV1CodecConfigurationRecord');
            return;
        }
        meta.profile = config.profile_string;
        meta.level = config.level_string;
        meta.bitDepth = config.bit_depth;
        meta.chromaFormat = config.chroma_format;
        meta.frameRate = config.frame_rate;
        if (config.frame_rate.fixed === false ||
            config.frame_rate.fps_num === 0 ||
            config.frame_rate.fps_den === 0) {
            meta.frameRate = this._referenceFrameRate;
        }
        var fps_den = meta.frameRate.fps_den;
        var fps_num = meta.frameRate.fps_num;
        meta.refSampleDuration = meta.timescale * (fps_den / fps_num);
        meta.codec = config.codec_mimetype;
        meta.extra = config;
        var mi = this._mediaInfo;
        mi.fps = meta.frameRate.fps;
        mi.profile = meta.profile;
        mi.level = meta.level;
        mi.refFrames = config.ref_frames;
        mi.chromaFormat = config.chroma_format_string;
        mi.videoCodec = config.codec_mimetype;
        if (mi.hasAudio) {
            if (mi.audioCodec != null) {
                mi.mimeType = 'video/x-flv; codecs="' + mi.videoCodec + ',' + mi.audioCodec + '"';
            }
        }
        else {
            mi.mimeType = 'video/x-flv; codecs="' + mi.videoCodec + '"';
        }
        if (mi.isComplete()) {
            this._onMediaInfo(mi);
        }
        meta.av1c = new Uint8Array(dataSize);
        meta.av1c.set(new Uint8Array(arrayBuffer, dataOffset, dataSize), 0);
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Preparing AV1CodecConfigurationRecord');
    };
    FLVDemuxer.prototype._parseAVCVideoData = function (arrayBuffer, dataOffset, dataSize, tagTimestamp, tagPosition, frameType, cts) {
        var le = this._littleEndian;
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        var units = [], length = 0;
        var offset = 0;
        var lengthSize = this._naluLengthSize;
        var dts = this._timestampBase + tagTimestamp;
        var keyframe = (frameType === 1); // from FLV Frame Type constants
        while (offset < dataSize) {
            if (offset + 4 >= dataSize) {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "Malformed Nalu near timestamp ".concat(dts, ", offset = ").concat(offset, ", dataSize = ").concat(dataSize));
                break; // data not enough for next Nalu
            }
            // Nalu with length-header (AVC1)
            var naluSize = v.getUint32(offset, !le); // Big-Endian read
            if (lengthSize === 3) {
                naluSize >>>= 8;
            }
            if (naluSize > dataSize - lengthSize) {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "Malformed Nalus near timestamp ".concat(dts, ", NaluSize > DataSize!"));
                return;
            }
            var unitType = v.getUint8(offset + lengthSize) & 0x1F;
            if (unitType === 5) { // IDR
                keyframe = true;
            }
            var data = new Uint8Array(arrayBuffer, dataOffset + offset, lengthSize + naluSize);
            var unit = { type: unitType, data: data };
            units.push(unit);
            length += data.byteLength;
            offset += lengthSize + naluSize;
        }
        if (units.length) {
            var track = this._videoTrack;
            var avcSample = {
                units: units,
                length: length,
                isKeyframe: keyframe,
                dts: dts,
                cts: cts,
                pts: (dts + cts)
            };
            if (keyframe) {
                avcSample.fileposition = tagPosition;
            }
            track.samples.push(avcSample);
            track.length += length;
        }
    };
    FLVDemuxer.prototype._parseHEVCVideoData = function (arrayBuffer, dataOffset, dataSize, tagTimestamp, tagPosition, frameType, cts) {
        var le = this._littleEndian;
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        var units = [], length = 0;
        var offset = 0;
        var lengthSize = this._naluLengthSize;
        var dts = this._timestampBase + tagTimestamp;
        var keyframe = (frameType === 1); // from FLV Frame Type constants
        while (offset < dataSize) {
            if (offset + 4 >= dataSize) {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "Malformed Nalu near timestamp ".concat(dts, ", offset = ").concat(offset, ", dataSize = ").concat(dataSize));
                break; // data not enough for next Nalu
            }
            // Nalu with length-header (HVC1)
            var naluSize = v.getUint32(offset, !le); // Big-Endian read
            if (lengthSize === 3) {
                naluSize >>>= 8;
            }
            if (naluSize > dataSize - lengthSize) {
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "Malformed Nalus near timestamp ".concat(dts, ", NaluSize > DataSize!"));
                return;
            }
            var unitType = (v.getUint8(offset + lengthSize) >> 1) & 0x3F;
            if (unitType === 19 || unitType === 20 || unitType === 21) { // IRAP
                keyframe = true;
            }
            var data = new Uint8Array(arrayBuffer, dataOffset + offset, lengthSize + naluSize);
            var unit = { type: unitType, data: data };
            units.push(unit);
            length += data.byteLength;
            offset += lengthSize + naluSize;
        }
        if (units.length) {
            var track = this._videoTrack;
            var hevcSample = {
                units: units,
                length: length,
                isKeyframe: keyframe,
                dts: dts,
                cts: cts,
                pts: (dts + cts)
            };
            if (keyframe) {
                hevcSample.fileposition = tagPosition;
            }
            track.samples.push(hevcSample);
            track.length += length;
        }
    };
    FLVDemuxer.prototype._parseAV1VideoData = function (arrayBuffer, dataOffset, dataSize, tagTimestamp, tagPosition, frameType, cts) {
        var le = this._littleEndian;
        var v = new DataView(arrayBuffer, dataOffset, dataSize);
        var units = [], length = 0;
        var offset = 0;
        var dts = this._timestampBase + tagTimestamp;
        var keyframe = (frameType === 1); // from FLV Frame Type constants
        if (keyframe) {
            var meta = this._videoMetadata;
            var config = _av1_parser_ts__WEBPACK_IMPORTED_MODULE_8__["default"].parseOBUs(new Uint8Array(arrayBuffer, dataOffset, dataSize), meta.extra);
            if (config == null) {
                this._onError(_demux_errors_js__WEBPACK_IMPORTED_MODULE_3__["default"].FORMAT_ERROR, 'Flv: Invalid AV1 VideoData');
                return;
            }
            console.log(config);
            meta.codecWidth = config.codec_size.width;
            meta.codecHeight = config.codec_size.height;
            meta.presentWidth = config.present_size.width;
            meta.presentHeight = config.present_size.height;
            meta.sarRatio = config.sar_ratio;
            var mi = this._mediaInfo;
            mi.width = meta.codecWidth;
            mi.height = meta.codecHeight;
            mi.sarNum = meta.sarRatio.width;
            mi.sarDen = meta.sarRatio.height;
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Parsed AV1DecoderConfigurationRecord');
            if (this._isInitialMetadataDispatched()) {
                // flush parsed frames
                if (this._dispatch && (this._audioTrack.length || this._videoTrack.length)) {
                    this._onDataAvailable(this._audioTrack, this._videoTrack);
                }
            }
            else {
                this._videoInitialMetadataDispatched = true;
            }
            // notify new metadata
            this._dispatch = false;
            this._onTrackMetadata('video', meta);
        }
        /* FIXME: NEEDS Inspect Per OBUs */
        length = dataSize;
        units.push({
            unitType: 0,
            data: new Uint8Array(arrayBuffer, dataOffset + offset, dataSize)
        });
        if (units.length) {
            var track = this._videoTrack;
            var av1Sample = {
                units: units,
                length: length,
                isKeyframe: keyframe,
                dts: dts,
                cts: cts,
                pts: (dts + cts)
            };
            if (keyframe) {
                av1Sample.fileposition = tagPosition;
            }
            track.samples.push(av1Sample);
            track.length += length;
        }
    };
    return FLVDemuxer;
}());
/* harmony default export */ __webpack_exports__["default"] = (FLVDemuxer);


/***/ }),

/***/ "./src/demux/h264.ts":
/*!***************************!*\
  !*** ./src/demux/h264.ts ***!
  \***************************/
/*! exports provided: H264NaluType, H264NaluPayload, H264NaluAVC1, H264AnnexBParser, AVCDecoderConfigurationRecord */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "H264NaluType", function() { return H264NaluType; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "H264NaluPayload", function() { return H264NaluPayload; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "H264NaluAVC1", function() { return H264NaluAVC1; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "H264AnnexBParser", function() { return H264AnnexBParser; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "AVCDecoderConfigurationRecord", function() { return AVCDecoderConfigurationRecord; });
/* harmony import */ var _utils_logger__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger */ "./src/utils/logger.js");

var H264NaluType;
(function (H264NaluType) {
    H264NaluType[H264NaluType["kUnspecified"] = 0] = "kUnspecified";
    H264NaluType[H264NaluType["kSliceNonIDR"] = 1] = "kSliceNonIDR";
    H264NaluType[H264NaluType["kSliceDPA"] = 2] = "kSliceDPA";
    H264NaluType[H264NaluType["kSliceDPB"] = 3] = "kSliceDPB";
    H264NaluType[H264NaluType["kSliceDPC"] = 4] = "kSliceDPC";
    H264NaluType[H264NaluType["kSliceIDR"] = 5] = "kSliceIDR";
    H264NaluType[H264NaluType["kSliceSEI"] = 6] = "kSliceSEI";
    H264NaluType[H264NaluType["kSliceSPS"] = 7] = "kSliceSPS";
    H264NaluType[H264NaluType["kSlicePPS"] = 8] = "kSlicePPS";
    H264NaluType[H264NaluType["kSliceAUD"] = 9] = "kSliceAUD";
    H264NaluType[H264NaluType["kEndOfSequence"] = 10] = "kEndOfSequence";
    H264NaluType[H264NaluType["kEndOfStream"] = 11] = "kEndOfStream";
    H264NaluType[H264NaluType["kFiller"] = 12] = "kFiller";
    H264NaluType[H264NaluType["kSPSExt"] = 13] = "kSPSExt";
    H264NaluType[H264NaluType["kReserved0"] = 14] = "kReserved0";
})(H264NaluType || (H264NaluType = {}));
var H264NaluPayload = /** @class */ (function () {
    function H264NaluPayload() {
    }
    return H264NaluPayload;
}());

var H264NaluAVC1 = /** @class */ (function () {
    function H264NaluAVC1(nalu) {
        var nalu_size = nalu.data.byteLength;
        this.type = nalu.type;
        this.data = new Uint8Array(4 + nalu_size); // 4 byte length-header + nalu payload
        var v = new DataView(this.data.buffer);
        // Fill 4 byte length-header
        v.setUint32(0, nalu_size);
        // Copy payload
        this.data.set(nalu.data, 4);
    }
    return H264NaluAVC1;
}());

var H264AnnexBParser = /** @class */ (function () {
    function H264AnnexBParser(data) {
        this.TAG = "H264AnnexBParser";
        this.current_startcode_offset_ = 0;
        this.eof_flag_ = false;
        this.data_ = data;
        this.current_startcode_offset_ = this.findNextStartCodeOffset(0);
        if (this.eof_flag_) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "Could not find H264 startcode until payload end!");
        }
    }
    H264AnnexBParser.prototype.findNextStartCodeOffset = function (start_offset) {
        var i = start_offset;
        var data = this.data_;
        while (true) {
            if (i + 3 >= data.byteLength) {
                this.eof_flag_ = true;
                return data.byteLength;
            }
            // search 00 00 00 01 or 00 00 01
            var uint32 = (data[i + 0] << 24)
                | (data[i + 1] << 16)
                | (data[i + 2] << 8)
                | (data[i + 3]);
            var uint24 = (data[i + 0] << 16)
                | (data[i + 1] << 8)
                | (data[i + 2]);
            if (uint32 === 0x00000001 || uint24 === 0x000001) {
                return i;
            }
            else {
                i++;
            }
        }
    };
    H264AnnexBParser.prototype.readNextNaluPayload = function () {
        var data = this.data_;
        var nalu_payload = null;
        while (nalu_payload == null) {
            if (this.eof_flag_) {
                break;
            }
            // offset pointed to start code
            var startcode_offset = this.current_startcode_offset_;
            // nalu payload start offset
            var offset = startcode_offset;
            var u32 = (data[offset] << 24) | (data[offset + 1] << 16) | (data[offset + 2] << 8) | (data[offset + 3]);
            if (u32 === 0x00000001) {
                offset += 4;
            }
            else {
                offset += 3;
            }
            var nalu_type = data[offset] & 0x1F;
            var forbidden_bit = (data[offset] & 0x80) >>> 7;
            var next_startcode_offset = this.findNextStartCodeOffset(offset);
            this.current_startcode_offset_ = next_startcode_offset;
            if (nalu_type >= H264NaluType.kReserved0) {
                continue;
            }
            if (forbidden_bit !== 0) {
                // Log.e(this.TAG, `forbidden_bit near offset ${offset} should be 0 but has value ${forbidden_bit}`);
                continue;
            }
            var payload_data = data.subarray(offset, next_startcode_offset);
            nalu_payload = new H264NaluPayload();
            nalu_payload.type = nalu_type;
            nalu_payload.data = payload_data;
        }
        return nalu_payload;
    };
    return H264AnnexBParser;
}());

var AVCDecoderConfigurationRecord = /** @class */ (function () {
    // sps, pps: require Nalu without 4 byte length-header
    function AVCDecoderConfigurationRecord(sps, pps, sps_details) {
        var length = 6 + 2 + sps.byteLength + 1 + 2 + pps.byteLength;
        var need_extra_fields = false;
        if (sps[3] !== 66 && sps[3] !== 77 && sps[3] !== 88) {
            need_extra_fields = true;
            length += 4;
        }
        var data = this.data = new Uint8Array(length);
        data[0] = 0x01; // configurationVersion
        data[1] = sps[1]; // AVCProfileIndication
        data[2] = sps[2]; // profile_compatibility
        data[3] = sps[3]; // AVCLevelIndication
        data[4] = 0xFF; // 111111 + lengthSizeMinusOne(3)
        data[5] = 0xE0 | 0x01; // 111 + numOfSequenceParameterSets
        var sps_length = sps.byteLength;
        data[6] = sps_length >>> 8; // sequenceParameterSetLength
        data[7] = sps_length & 0xFF;
        var offset = 8;
        data.set(sps, 8);
        offset += sps_length;
        data[offset] = 1; // numOfPictureParameterSets
        var pps_length = pps.byteLength;
        data[offset + 1] = pps_length >>> 8; // pictureParameterSetLength
        data[offset + 2] = pps_length & 0xFF;
        data.set(pps, offset + 3);
        offset += 3 + pps_length;
        if (need_extra_fields) {
            data[offset] = 0xFC | sps_details.chroma_format_idc;
            data[offset + 1] = 0xF8 | (sps_details.bit_depth_luma - 8);
            data[offset + 2] = 0xF8 | (sps_details.bit_depth_chroma - 8);
            data[offset + 3] = 0x00; // number of sps ext
            offset += 4;
        }
    }
    AVCDecoderConfigurationRecord.prototype.getData = function () {
        return this.data;
    };
    return AVCDecoderConfigurationRecord;
}());



/***/ }),

/***/ "./src/demux/h265-parser.js":
/*!**********************************!*\
  !*** ./src/demux/h265-parser.js ***!
  \**********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _exp_golomb_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ./exp-golomb.js */ "./src/demux/exp-golomb.js");
/*
 * Copyright (C) 2022 もにょてっく. All Rights Reserved.
 *
 * @author もにょ〜ん <monyone.teihen@gmail.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

var H265NaluParser = /** @class */ (function () {
    function H265NaluParser() {
    }
    H265NaluParser._ebsp2rbsp = function (uint8array) {
        var src = uint8array;
        var src_length = src.byteLength;
        var dst = new Uint8Array(src_length);
        var dst_idx = 0;
        for (var i = 0; i < src_length; i++) {
            if (i >= 2) {
                // Unescape: Skip 0x03 after 00 00
                if (src[i] === 0x03 && src[i - 1] === 0x00 && src[i - 2] === 0x00) {
                    continue;
                }
            }
            dst[dst_idx] = src[i];
            dst_idx++;
        }
        return new Uint8Array(dst.buffer, 0, dst_idx);
    };
    H265NaluParser.parseVPS = function (uint8array) {
        var rbsp = H265NaluParser._ebsp2rbsp(uint8array);
        var gb = new _exp_golomb_js__WEBPACK_IMPORTED_MODULE_0__["default"](rbsp);
        /* remove NALu Header */
        gb.readByte();
        gb.readByte();
        // VPS
        var video_parameter_set_id = gb.readBits(4);
        gb.readBits(2);
        var max_layers_minus1 = gb.readBits(6);
        var max_sub_layers_minus1 = gb.readBits(3);
        var temporal_id_nesting_flag = gb.readBool();
        // and more ...
        return {
            num_temporal_layers: max_sub_layers_minus1 + 1,
            temporal_id_nested: temporal_id_nesting_flag
        };
    };
    H265NaluParser.parseSPS = function (uint8array) {
        var rbsp = H265NaluParser._ebsp2rbsp(uint8array);
        var gb = new _exp_golomb_js__WEBPACK_IMPORTED_MODULE_0__["default"](rbsp);
        /* remove NALu Header */
        gb.readByte();
        gb.readByte();
        var left_offset = 0, right_offset = 0, top_offset = 0, bottom_offset = 0;
        // SPS
        var video_paramter_set_id = gb.readBits(4);
        var max_sub_layers_minus1 = gb.readBits(3);
        var temporal_id_nesting_flag = gb.readBool();
        // profile_tier_level begin
        var general_profile_space = gb.readBits(2);
        var general_tier_flag = gb.readBool();
        var general_profile_idc = gb.readBits(5);
        var general_profile_compatibility_flags_1 = gb.readByte();
        var general_profile_compatibility_flags_2 = gb.readByte();
        var general_profile_compatibility_flags_3 = gb.readByte();
        var general_profile_compatibility_flags_4 = gb.readByte();
        var general_constraint_indicator_flags_1 = gb.readByte();
        var general_constraint_indicator_flags_2 = gb.readByte();
        var general_constraint_indicator_flags_3 = gb.readByte();
        var general_constraint_indicator_flags_4 = gb.readByte();
        var general_constraint_indicator_flags_5 = gb.readByte();
        var general_constraint_indicator_flags_6 = gb.readByte();
        var general_level_idc = gb.readByte();
        var sub_layer_profile_present_flag = [];
        var sub_layer_level_present_flag = [];
        for (var i = 0; i < max_sub_layers_minus1; i++) {
            sub_layer_profile_present_flag.push(gb.readBool());
            sub_layer_level_present_flag.push(gb.readBool());
        }
        if (max_sub_layers_minus1 > 0) {
            for (var i = max_sub_layers_minus1; i < 8; i++) {
                gb.readBits(2);
            }
        }
        for (var i = 0; i < max_sub_layers_minus1; i++) {
            if (sub_layer_profile_present_flag[i]) {
                gb.readByte(); // sub_layer_profile_space, sub_layer_tier_flag, sub_layer_profile_idc
                gb.readByte();
                gb.readByte();
                gb.readByte();
                gb.readByte(); // sub_layer_profile_compatibility_flag
                gb.readByte();
                gb.readByte();
                gb.readByte();
                gb.readByte();
                gb.readByte();
                gb.readByte();
            }
            if (sub_layer_level_present_flag[i]) {
                gb.readByte();
            }
        }
        // profile_tier_level end
        var seq_parameter_set_id = gb.readUEG();
        var chroma_format_idc = gb.readUEG();
        if (chroma_format_idc == 3) {
            gb.readBits(1); // separate_colour_plane_flag
        }
        var pic_width_in_luma_samples = gb.readUEG();
        var pic_height_in_luma_samples = gb.readUEG();
        var conformance_window_flag = gb.readBool();
        if (conformance_window_flag) {
            left_offset += gb.readUEG();
            right_offset += gb.readUEG();
            top_offset += gb.readUEG();
            bottom_offset += gb.readUEG();
        }
        var bit_depth_luma_minus8 = gb.readUEG();
        var bit_depth_chroma_minus8 = gb.readUEG();
        var log2_max_pic_order_cnt_lsb_minus4 = gb.readUEG();
        var sub_layer_ordering_info_present_flag = gb.readBool();
        for (var i = sub_layer_ordering_info_present_flag ? 0 : max_sub_layers_minus1; i <= max_sub_layers_minus1; i++) {
            gb.readUEG(); // max_dec_pic_buffering_minus1[i]
            gb.readUEG(); // max_num_reorder_pics[i]
            gb.readUEG(); // max_latency_increase_plus1[i]
        }
        var log2_min_luma_coding_block_size_minus3 = gb.readUEG();
        var log2_diff_max_min_luma_coding_block_size = gb.readUEG();
        var log2_min_transform_block_size_minus2 = gb.readUEG();
        var log2_diff_max_min_transform_block_size = gb.readUEG();
        var max_transform_hierarchy_depth_inter = gb.readUEG();
        var max_transform_hierarchy_depth_intra = gb.readUEG();
        var scaling_list_enabled_flag = gb.readBool();
        if (scaling_list_enabled_flag) {
            var sps_scaling_list_data_present_flag = gb.readBool();
            if (sps_scaling_list_data_present_flag) {
                for (var sizeId = 0; sizeId < 4; sizeId++) {
                    for (var matrixId = 0; matrixId < ((sizeId === 3) ? 2 : 6); matrixId++) {
                        var scaling_list_pred_mode_flag = gb.readBool();
                        if (!scaling_list_pred_mode_flag) {
                            gb.readUEG(); // scaling_list_pred_matrix_id_delta
                        }
                        else {
                            var coefNum = Math.min(64, (1 << (4 + (sizeId << 1))));
                            if (sizeId > 1) {
                                gb.readSEG();
                            }
                            for (var i = 0; i < coefNum; i++) {
                                gb.readSEG();
                            }
                        }
                    }
                }
            }
        }
        var amp_enabled_flag = gb.readBool();
        var sample_adaptive_offset_enabled_flag = gb.readBool();
        var pcm_enabled_flag = gb.readBool();
        if (pcm_enabled_flag) {
            gb.readByte();
            gb.readUEG();
            gb.readUEG();
            gb.readBool();
        }
        var num_short_term_ref_pic_sets = gb.readUEG();
        var num_delta_pocs = 0;
        for (var i = 0; i < num_short_term_ref_pic_sets; i++) {
            var inter_ref_pic_set_prediction_flag = false;
            if (i !== 0) {
                inter_ref_pic_set_prediction_flag = gb.readBool();
            }
            if (inter_ref_pic_set_prediction_flag) {
                if (i === num_short_term_ref_pic_sets) {
                    gb.readUEG();
                }
                gb.readBool();
                gb.readUEG();
                var next_num_delta_pocs = 0;
                for (var j = 0; j <= num_delta_pocs; j++) {
                    var used_by_curr_pic_flag = gb.readBool();
                    var use_delta_flag = false;
                    if (!used_by_curr_pic_flag) {
                        use_delta_flag = gb.readBool();
                    }
                    if (used_by_curr_pic_flag || use_delta_flag) {
                        next_num_delta_pocs++;
                    }
                }
                num_delta_pocs = next_num_delta_pocs;
            }
            else {
                var num_negative_pics = gb.readUEG();
                var num_positive_pics = gb.readUEG();
                num_delta_pocs = num_negative_pics + num_positive_pics;
                for (var j = 0; j < num_negative_pics; j++) {
                    gb.readUEG();
                    gb.readBool();
                }
                for (var j = 0; j < num_positive_pics; j++) {
                    gb.readUEG();
                    gb.readBool();
                }
            }
        }
        var long_term_ref_pics_present_flag = gb.readBool();
        if (long_term_ref_pics_present_flag) {
            var num_long_term_ref_pics_sps = gb.readUEG();
            for (var i = 0; i < num_long_term_ref_pics_sps; i++) {
                for (var j = 0; j < (log2_max_pic_order_cnt_lsb_minus4 + 4); j++) {
                    gb.readBits(1);
                }
                gb.readBits(1);
            }
        }
        //*
        var default_display_window_flag = false; // for calc offset
        var min_spatial_segmentation_idc = 0; // for hvcC
        var sar_width = 1, sar_height = 1;
        var fps_fixed = false, fps_den = 1, fps_num = 1;
        //*/
        var sps_temporal_mvp_enabled_flag = gb.readBool();
        var strong_intra_smoothing_enabled_flag = gb.readBool();
        var vui_parameters_present_flag = gb.readBool();
        if (vui_parameters_present_flag) {
            var aspect_ratio_info_present_flag = gb.readBool();
            if (aspect_ratio_info_present_flag) {
                var aspect_ratio_idc = gb.readByte();
                var sar_w_table = [1, 12, 10, 16, 40, 24, 20, 32, 80, 18, 15, 64, 160, 4, 3, 2];
                var sar_h_table = [1, 11, 11, 11, 33, 11, 11, 11, 33, 11, 11, 33, 99, 3, 2, 1];
                if (aspect_ratio_idc > 0 && aspect_ratio_idc <= 16) {
                    sar_width = sar_w_table[aspect_ratio_idc - 1];
                    sar_height = sar_h_table[aspect_ratio_idc - 1];
                }
                else if (aspect_ratio_idc === 255) {
                    sar_width = gb.readBits(16);
                    sar_height = gb.readBits(16);
                }
            }
            var overscan_info_present_flag = gb.readBool();
            if (overscan_info_present_flag) {
                gb.readBool();
            }
            var video_signal_type_present_flag = gb.readBool();
            if (video_signal_type_present_flag) {
                gb.readBits(3);
                gb.readBool();
                var colour_description_present_flag = gb.readBool();
                if (colour_description_present_flag) {
                    gb.readByte();
                    gb.readByte();
                    gb.readByte();
                }
            }
            var chroma_loc_info_present_flag = gb.readBool();
            if (chroma_loc_info_present_flag) {
                gb.readUEG();
                gb.readUEG();
            }
            var neutral_chroma_indication_flag = gb.readBool();
            var field_seq_flag = gb.readBool();
            var frame_field_info_present_flag = gb.readBool();
            default_display_window_flag = gb.readBool();
            if (default_display_window_flag) {
                gb.readUEG();
                gb.readUEG();
                gb.readUEG();
                gb.readUEG();
            }
            var vui_timing_info_present_flag = gb.readBool();
            if (vui_timing_info_present_flag) {
                fps_den = gb.readBits(32);
                fps_num = gb.readBits(32);
                var vui_poc_proportional_to_timing_flag = gb.readBool();
                if (vui_poc_proportional_to_timing_flag) {
                    gb.readUEG();
                }
                var vui_hrd_parameters_present_flag = gb.readBool();
                if (vui_hrd_parameters_present_flag) {
                    var commonInfPresentFlag = 1;
                    var nal_hrd_parameters_present_flag = false;
                    var vcl_hrd_parameters_present_flag = false;
                    var sub_pic_hrd_params_present_flag = false;
                    if (commonInfPresentFlag) {
                        nal_hrd_parameters_present_flag = gb.readBool();
                        vcl_hrd_parameters_present_flag = gb.readBool();
                        if (nal_hrd_parameters_present_flag || vcl_hrd_parameters_present_flag) {
                            sub_pic_hrd_params_present_flag = gb.readBool();
                            if (sub_pic_hrd_params_present_flag) {
                                gb.readByte();
                                gb.readBits(5);
                                gb.readBool();
                                gb.readBits(5);
                            }
                            var bit_rate_scale = gb.readBits(4);
                            var cpb_size_scale = gb.readBits(4);
                            if (sub_pic_hrd_params_present_flag) {
                                gb.readBits(4);
                            }
                            gb.readBits(5);
                            gb.readBits(5);
                            gb.readBits(5);
                        }
                    }
                    for (var i = 0; i <= max_sub_layers_minus1; i++) {
                        var fixed_pic_rate_general_flag = gb.readBool();
                        fps_fixed = fixed_pic_rate_general_flag;
                        var fixed_pic_rate_within_cvs_flag = true;
                        var cpbCnt = 1;
                        if (!fixed_pic_rate_general_flag) {
                            fixed_pic_rate_within_cvs_flag = gb.readBool();
                        }
                        var low_delay_hrd_flag = false;
                        if (fixed_pic_rate_within_cvs_flag) {
                            gb.readUEG();
                        }
                        else {
                            low_delay_hrd_flag = gb.readBool();
                        }
                        if (!low_delay_hrd_flag) {
                            cpbCnt = gb.readUEG() + 1;
                        }
                        if (nal_hrd_parameters_present_flag) {
                            for (var j = 0; j < cpbCnt; j++) {
                                gb.readUEG();
                                gb.readUEG();
                                if (sub_pic_hrd_params_present_flag) {
                                    gb.readUEG();
                                    gb.readUEG();
                                }
                            }
                            gb.readBool();
                        }
                        if (vcl_hrd_parameters_present_flag) {
                            for (var j = 0; j < cpbCnt; j++) {
                                gb.readUEG();
                                gb.readUEG();
                                if (sub_pic_hrd_params_present_flag) {
                                    gb.readUEG();
                                    gb.readUEG();
                                }
                            }
                            gb.readBool();
                        }
                    }
                }
            }
            var bitstream_restriction_flag = gb.readBool();
            if (bitstream_restriction_flag) {
                var tiles_fixed_structure_flag = gb.readBool();
                var motion_vectors_over_pic_boundaries_flag = gb.readBool();
                var restricted_ref_pic_lists_flag = gb.readBool();
                min_spatial_segmentation_idc = gb.readUEG();
                var max_bytes_per_pic_denom = gb.readUEG();
                var max_bits_per_min_cu_denom = gb.readUEG();
                var log2_max_mv_length_horizontal = gb.readUEG();
                var log2_max_mv_length_vertical = gb.readUEG();
            }
        }
        var sps_extension_flag = gb.readBool(); // ignore...
        // for meta data
        var codec_mimetype = "hvc1.".concat(general_profile_idc, ".1.L").concat(general_level_idc, ".B0");
        var sub_wc = (chroma_format_idc === 1 || chroma_format_idc === 2) ? 2 : 1;
        var sub_hc = (chroma_format_idc === 1) ? 2 : 1;
        var codec_width = pic_width_in_luma_samples - (left_offset + right_offset) * sub_wc;
        var codec_height = pic_height_in_luma_samples - (top_offset + bottom_offset) * sub_hc;
        var sar_scale = 1;
        if (sar_width !== 1 && sar_height !== 1) {
            sar_scale = sar_width / sar_height;
        }
        gb.destroy();
        gb = null;
        return {
            codec_mimetype: codec_mimetype,
            profile_string: H265NaluParser.getProfileString(general_profile_idc),
            level_string: H265NaluParser.getLevelString(general_level_idc),
            profile_idc: general_profile_idc,
            bit_depth: bit_depth_luma_minus8 + 8,
            ref_frames: 1,
            chroma_format: chroma_format_idc,
            chroma_format_string: H265NaluParser.getChromaFormatString(chroma_format_idc),
            general_level_idc: general_level_idc,
            general_profile_space: general_profile_space,
            general_tier_flag: general_tier_flag,
            general_profile_idc: general_profile_idc,
            general_profile_compatibility_flags_1: general_profile_compatibility_flags_1,
            general_profile_compatibility_flags_2: general_profile_compatibility_flags_2,
            general_profile_compatibility_flags_3: general_profile_compatibility_flags_3,
            general_profile_compatibility_flags_4: general_profile_compatibility_flags_4,
            general_constraint_indicator_flags_1: general_constraint_indicator_flags_1,
            general_constraint_indicator_flags_2: general_constraint_indicator_flags_2,
            general_constraint_indicator_flags_3: general_constraint_indicator_flags_3,
            general_constraint_indicator_flags_4: general_constraint_indicator_flags_4,
            general_constraint_indicator_flags_5: general_constraint_indicator_flags_5,
            general_constraint_indicator_flags_6: general_constraint_indicator_flags_6,
            min_spatial_segmentation_idc: min_spatial_segmentation_idc,
            constant_frame_rate: 0 /* FIXME!! fps_fixed ? 1 : 0? */,
            chroma_format_idc: chroma_format_idc,
            bit_depth_luma_minus8: bit_depth_luma_minus8,
            bit_depth_chroma_minus8: bit_depth_chroma_minus8,
            frame_rate: {
                fixed: fps_fixed,
                fps: fps_num / fps_den,
                fps_den: fps_den,
                fps_num: fps_num,
            },
            sar_ratio: {
                width: sar_width,
                height: sar_height
            },
            codec_size: {
                width: codec_width,
                height: codec_height
            },
            present_size: {
                width: codec_width * sar_scale,
                height: codec_height
            }
        };
    };
    H265NaluParser.parsePPS = function (uint8array) {
        var rbsp = H265NaluParser._ebsp2rbsp(uint8array);
        var gb = new _exp_golomb_js__WEBPACK_IMPORTED_MODULE_0__["default"](rbsp);
        /* remove NALu Header */
        gb.readByte();
        gb.readByte();
        var pic_parameter_set_id = gb.readUEG();
        var seq_parameter_set_id = gb.readUEG();
        var dependent_slice_segments_enabled_flag = gb.readBool();
        var output_flag_present_flag = gb.readBool();
        var num_extra_slice_header_bits = gb.readBits(3);
        var sign_data_hiding_enabled_flag = gb.readBool();
        var cabac_init_present_flag = gb.readBool();
        var num_ref_idx_l0_default_active_minus1 = gb.readUEG();
        var num_ref_idx_l1_default_active_minus1 = gb.readUEG();
        var init_qp_minus26 = gb.readSEG();
        var constrained_intra_pred_flag = gb.readBool();
        var transform_skip_enabled_flag = gb.readBool();
        var cu_qp_delta_enabled_flag = gb.readBool();
        if (cu_qp_delta_enabled_flag) {
            var diff_cu_qp_delta_depth = gb.readUEG();
        }
        var cb_qp_offset = gb.readSEG();
        var cr_qp_offset = gb.readSEG();
        var pps_slice_chroma_qp_offsets_present_flag = gb.readBool();
        var weighted_pred_flag = gb.readBool();
        var weighted_bipred_flag = gb.readBool();
        var transquant_bypass_enabled_flag = gb.readBool();
        var tiles_enabled_flag = gb.readBool();
        var entropy_coding_sync_enabled_flag = gb.readBool();
        // and more ...
        // needs hvcC
        var parallelismType = 1; // slice-based parallel decoding
        if (entropy_coding_sync_enabled_flag && tiles_enabled_flag) {
            parallelismType = 0; // mixed-type parallel decoding
        }
        else if (entropy_coding_sync_enabled_flag) {
            parallelismType = 3; // wavefront-based parallel decoding
        }
        else if (tiles_enabled_flag) {
            parallelismType = 2; // tile-based parallel decoding
        }
        return {
            parallelismType: parallelismType
        };
    };
    H265NaluParser.getChromaFormatString = function (chroma_idc) {
        switch (chroma_idc) {
            case 0: return '4:0:0';
            case 1: return '4:2:0';
            case 2: return '4:2:2';
            case 3: return '4:4:4';
            default: return 'Unknown';
        }
    };
    H265NaluParser.getProfileString = function (profile_idc) {
        switch (profile_idc) {
            case 1: return 'Main';
            case 2: return 'Main10';
            case 3: return 'MainSP';
            case 4: return 'Rext';
            case 9: return 'SCC';
            default: return 'Unknown';
        }
    };
    H265NaluParser.getLevelString = function (level_idc) {
        return (level_idc / 30).toFixed(1);
    };
    return H265NaluParser;
}());
/* harmony default export */ __webpack_exports__["default"] = (H265NaluParser);


/***/ }),

/***/ "./src/demux/h265.ts":
/*!***************************!*\
  !*** ./src/demux/h265.ts ***!
  \***************************/
/*! exports provided: H265NaluType, H265NaluPayload, H265NaluHVC1, H265AnnexBParser, HEVCDecoderConfigurationRecord */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "H265NaluType", function() { return H265NaluType; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "H265NaluPayload", function() { return H265NaluPayload; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "H265NaluHVC1", function() { return H265NaluHVC1; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "H265AnnexBParser", function() { return H265AnnexBParser; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "HEVCDecoderConfigurationRecord", function() { return HEVCDecoderConfigurationRecord; });
/* harmony import */ var _utils_logger__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger */ "./src/utils/logger.js");

var H265NaluType;
(function (H265NaluType) {
    H265NaluType[H265NaluType["kSliceIDR_W_RADL"] = 19] = "kSliceIDR_W_RADL";
    H265NaluType[H265NaluType["kSliceIDR_N_LP"] = 20] = "kSliceIDR_N_LP";
    H265NaluType[H265NaluType["kSliceCRA_NUT"] = 21] = "kSliceCRA_NUT";
    H265NaluType[H265NaluType["kSliceVPS"] = 32] = "kSliceVPS";
    H265NaluType[H265NaluType["kSliceSPS"] = 33] = "kSliceSPS";
    H265NaluType[H265NaluType["kSlicePPS"] = 34] = "kSlicePPS";
    H265NaluType[H265NaluType["kSliceAUD"] = 35] = "kSliceAUD";
})(H265NaluType || (H265NaluType = {}));
var H265NaluPayload = /** @class */ (function () {
    function H265NaluPayload() {
    }
    return H265NaluPayload;
}());

var H265NaluHVC1 = /** @class */ (function () {
    function H265NaluHVC1(nalu) {
        var nalu_size = nalu.data.byteLength;
        this.type = nalu.type;
        this.data = new Uint8Array(4 + nalu_size); // 4 byte length-header + nalu payload
        var v = new DataView(this.data.buffer);
        // Fill 4 byte length-header
        v.setUint32(0, nalu_size);
        // Copy payload
        this.data.set(nalu.data, 4);
    }
    return H265NaluHVC1;
}());

var H265AnnexBParser = /** @class */ (function () {
    function H265AnnexBParser(data) {
        this.TAG = "H265AnnexBParser";
        this.current_startcode_offset_ = 0;
        this.eof_flag_ = false;
        this.data_ = data;
        this.current_startcode_offset_ = this.findNextStartCodeOffset(0);
        if (this.eof_flag_) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "Could not find H265 startcode until payload end!");
        }
    }
    H265AnnexBParser.prototype.findNextStartCodeOffset = function (start_offset) {
        var i = start_offset;
        var data = this.data_;
        while (true) {
            if (i + 3 >= data.byteLength) {
                this.eof_flag_ = true;
                return data.byteLength;
            }
            // search 00 00 00 01 or 00 00 01
            var uint32 = (data[i + 0] << 24)
                | (data[i + 1] << 16)
                | (data[i + 2] << 8)
                | (data[i + 3]);
            var uint24 = (data[i + 0] << 16)
                | (data[i + 1] << 8)
                | (data[i + 2]);
            if (uint32 === 0x00000001 || uint24 === 0x000001) {
                return i;
            }
            else {
                i++;
            }
        }
    };
    H265AnnexBParser.prototype.readNextNaluPayload = function () {
        var data = this.data_;
        var nalu_payload = null;
        while (nalu_payload == null) {
            if (this.eof_flag_) {
                break;
            }
            // offset pointed to start code
            var startcode_offset = this.current_startcode_offset_;
            // nalu payload start offset
            var offset = startcode_offset;
            var u32 = (data[offset] << 24) | (data[offset + 1] << 16) | (data[offset + 2] << 8) | (data[offset + 3]);
            if (u32 === 0x00000001) {
                offset += 4;
            }
            else {
                offset += 3;
            }
            var nalu_type = (data[offset] >> 1) & 0x3F;
            var forbidden_bit = (data[offset] & 0x80) >>> 7;
            var next_startcode_offset = this.findNextStartCodeOffset(offset);
            this.current_startcode_offset_ = next_startcode_offset;
            if (forbidden_bit !== 0) {
                // Log.e(this.TAG, `forbidden_bit near offset ${offset} should be 0 but has value ${forbidden_bit}`);
                continue;
            }
            var payload_data = data.subarray(offset, next_startcode_offset);
            nalu_payload = new H265NaluPayload();
            nalu_payload.type = nalu_type;
            nalu_payload.data = payload_data;
        }
        return nalu_payload;
    };
    return H265AnnexBParser;
}());

var HEVCDecoderConfigurationRecord = /** @class */ (function () {
    // sps, pps: require Nalu without 4 byte length-header
    function HEVCDecoderConfigurationRecord(vps, sps, pps, detail) {
        var length = 23 + (3 + 2 + vps.byteLength) + (3 + 2 + sps.byteLength) + (3 + 2 + pps.byteLength);
        var data = this.data = new Uint8Array(length);
        data[0] = 0x01; // configurationVersion
        data[1] = ((detail.general_profile_space & 0x03) << 6) | ((detail.general_tier_flag ? 1 : 0) << 5) | ((detail.general_profile_idc & 0x1F));
        data[2] = detail.general_profile_compatibility_flags_1;
        data[3] = detail.general_profile_compatibility_flags_2;
        data[4] = detail.general_profile_compatibility_flags_3;
        data[5] = detail.general_profile_compatibility_flags_4;
        data[6] = detail.general_constraint_indicator_flags_1;
        data[7] = detail.general_constraint_indicator_flags_2;
        data[8] = detail.general_constraint_indicator_flags_3;
        data[9] = detail.general_constraint_indicator_flags_4;
        data[10] = detail.general_constraint_indicator_flags_5;
        data[11] = detail.general_constraint_indicator_flags_6;
        data[12] = detail.general_level_idc;
        data[13] = 0xF0 | ((detail.min_spatial_segmentation_idc & 0x0F00) >> 8);
        data[14] = (detail.min_spatial_segmentation_idc & 0xFF);
        data[15] = 0xFC | (detail.parallelismType & 0x03);
        data[16] = 0xFC | (detail.chroma_format_idc & 0x03);
        data[17] = 0xF8 | (detail.bit_depth_luma_minus8 & 0x07);
        data[18] = 0xF8 | (detail.bit_depth_chroma_minus8 & 0x07);
        data[19] = 0;
        data[20] = 0;
        data[21] = ((detail.constant_frame_rate & 0x03) << 6) | ((detail.num_temporal_layers & 0x07) << 3) | ((detail.temporal_id_nested ? 1 : 0) << 2) | 3;
        data[22] = 3;
        data[23 + 0 + 0] = 0x80 | H265NaluType.kSliceVPS;
        data[23 + 0 + 1] = 0;
        data[23 + 0 + 2] = 1;
        data[23 + 0 + 3] = (vps.byteLength & 0xFF00) >> 8;
        data[23 + 0 + 4] = (vps.byteLength & 0x00FF) >> 0;
        data.set(vps, 23 + 0 + 5);
        data[23 + (5 + vps.byteLength) + 0] = 0x80 | H265NaluType.kSliceSPS;
        data[23 + (5 + vps.byteLength) + 1] = 0;
        data[23 + (5 + vps.byteLength) + 2] = 1;
        data[23 + (5 + vps.byteLength) + 3] = (sps.byteLength & 0xFF00) >> 8;
        data[23 + (5 + vps.byteLength) + 4] = (sps.byteLength & 0x00FF) >> 0;
        data.set(sps, 23 + (5 + vps.byteLength) + 5);
        data[23 + (5 + vps.byteLength + 5 + sps.byteLength) + 0] = 0x80 | H265NaluType.kSlicePPS;
        data[23 + (5 + vps.byteLength + 5 + sps.byteLength) + 1] = 0;
        data[23 + (5 + vps.byteLength + 5 + sps.byteLength) + 2] = 1;
        data[23 + (5 + vps.byteLength + 5 + sps.byteLength) + 3] = (pps.byteLength & 0xFF00) >> 8;
        data[23 + (5 + vps.byteLength + 5 + sps.byteLength) + 4] = (pps.byteLength & 0x00FF) >> 0;
        data.set(pps, 23 + (5 + vps.byteLength + 5 + sps.byteLength) + 5);
    }
    HEVCDecoderConfigurationRecord.prototype.getData = function () {
        return this.data;
    };
    return HEVCDecoderConfigurationRecord;
}());



/***/ }),

/***/ "./src/demux/klv.ts":
/*!**************************!*\
  !*** ./src/demux/klv.ts ***!
  \**************************/
/*! exports provided: KLVData, klv_parse */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "KLVData", function() { return KLVData; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "klv_parse", function() { return klv_parse; });
var KLVData = /** @class */ (function () {
    function KLVData() {
    }
    return KLVData;
}());

var klv_parse = function (data) {
    var result = [];
    var offset = 0;
    while (offset + 5 < data.byteLength) {
        var service_id = data[offset + 0];
        var sequence_number = data[offset + 1];
        var flags = data[offset + 2];
        var au_size = (data[offset + 3] << 8) | (data[offset + 4] << 0);
        var au_data = data.slice(offset + 5, offset + 5 + au_size);
        result.push({
            service_id: service_id,
            sequence_number: sequence_number,
            flags: flags,
            data: au_data
        });
        offset += 5 + au_size;
    }
    return result;
};


/***/ }),

/***/ "./src/demux/mp3.ts":
/*!**************************!*\
  !*** ./src/demux/mp3.ts ***!
  \**************************/
/*! exports provided: MP3Data */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "MP3Data", function() { return MP3Data; });
var MP3Data = /** @class */ (function () {
    function MP3Data() {
    }
    return MP3Data;
}());



/***/ }),

/***/ "./src/demux/mpeg4-audio.ts":
/*!**********************************!*\
  !*** ./src/demux/mpeg4-audio.ts ***!
  \**********************************/
/*! exports provided: MPEG4AudioObjectTypes, MPEG4SamplingFrequencyIndex, MPEG4SamplingFrequencies */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "MPEG4AudioObjectTypes", function() { return MPEG4AudioObjectTypes; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "MPEG4SamplingFrequencyIndex", function() { return MPEG4SamplingFrequencyIndex; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "MPEG4SamplingFrequencies", function() { return MPEG4SamplingFrequencies; });
var MPEG4AudioObjectTypes;
(function (MPEG4AudioObjectTypes) {
    MPEG4AudioObjectTypes[MPEG4AudioObjectTypes["kNull"] = 0] = "kNull";
    MPEG4AudioObjectTypes[MPEG4AudioObjectTypes["kAACMain"] = 1] = "kAACMain";
    MPEG4AudioObjectTypes[MPEG4AudioObjectTypes["kAAC_LC"] = 2] = "kAAC_LC";
    MPEG4AudioObjectTypes[MPEG4AudioObjectTypes["kAAC_SSR"] = 3] = "kAAC_SSR";
    MPEG4AudioObjectTypes[MPEG4AudioObjectTypes["kAAC_LTP"] = 4] = "kAAC_LTP";
    MPEG4AudioObjectTypes[MPEG4AudioObjectTypes["kAAC_SBR"] = 5] = "kAAC_SBR";
    MPEG4AudioObjectTypes[MPEG4AudioObjectTypes["kAAC_Scalable"] = 6] = "kAAC_Scalable";
    MPEG4AudioObjectTypes[MPEG4AudioObjectTypes["kLayer1"] = 32] = "kLayer1";
    MPEG4AudioObjectTypes[MPEG4AudioObjectTypes["kLayer2"] = 33] = "kLayer2";
    MPEG4AudioObjectTypes[MPEG4AudioObjectTypes["kLayer3"] = 34] = "kLayer3";
})(MPEG4AudioObjectTypes || (MPEG4AudioObjectTypes = {}));
var MPEG4SamplingFrequencyIndex;
(function (MPEG4SamplingFrequencyIndex) {
    MPEG4SamplingFrequencyIndex[MPEG4SamplingFrequencyIndex["k96000Hz"] = 0] = "k96000Hz";
    MPEG4SamplingFrequencyIndex[MPEG4SamplingFrequencyIndex["k88200Hz"] = 1] = "k88200Hz";
    MPEG4SamplingFrequencyIndex[MPEG4SamplingFrequencyIndex["k64000Hz"] = 2] = "k64000Hz";
    MPEG4SamplingFrequencyIndex[MPEG4SamplingFrequencyIndex["k48000Hz"] = 3] = "k48000Hz";
    MPEG4SamplingFrequencyIndex[MPEG4SamplingFrequencyIndex["k44100Hz"] = 4] = "k44100Hz";
    MPEG4SamplingFrequencyIndex[MPEG4SamplingFrequencyIndex["k32000Hz"] = 5] = "k32000Hz";
    MPEG4SamplingFrequencyIndex[MPEG4SamplingFrequencyIndex["k24000Hz"] = 6] = "k24000Hz";
    MPEG4SamplingFrequencyIndex[MPEG4SamplingFrequencyIndex["k22050Hz"] = 7] = "k22050Hz";
    MPEG4SamplingFrequencyIndex[MPEG4SamplingFrequencyIndex["k16000Hz"] = 8] = "k16000Hz";
    MPEG4SamplingFrequencyIndex[MPEG4SamplingFrequencyIndex["k12000Hz"] = 9] = "k12000Hz";
    MPEG4SamplingFrequencyIndex[MPEG4SamplingFrequencyIndex["k11025Hz"] = 10] = "k11025Hz";
    MPEG4SamplingFrequencyIndex[MPEG4SamplingFrequencyIndex["k8000Hz"] = 11] = "k8000Hz";
    MPEG4SamplingFrequencyIndex[MPEG4SamplingFrequencyIndex["k7350Hz"] = 12] = "k7350Hz";
})(MPEG4SamplingFrequencyIndex || (MPEG4SamplingFrequencyIndex = {}));
var MPEG4SamplingFrequencies = [
    96000,
    88200,
    64000,
    48000,
    44100,
    32000,
    24000,
    22050,
    16000,
    12000,
    11025,
    8000,
    7350,
];


/***/ }),

/***/ "./src/demux/pat-pmt-pes.ts":
/*!**********************************!*\
  !*** ./src/demux/pat-pmt-pes.ts ***!
  \**********************************/
/*! exports provided: PAT, StreamType, PMT, PESData, SectionData, SliceQueue */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "PAT", function() { return PAT; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "StreamType", function() { return StreamType; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "PMT", function() { return PMT; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "PESData", function() { return PESData; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "SectionData", function() { return SectionData; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "SliceQueue", function() { return SliceQueue; });
var PAT = /** @class */ (function () {
    function PAT() {
        // program_number -> pmt_pid
        this.program_pmt_pid = {};
    }
    return PAT;
}());

var StreamType;
(function (StreamType) {
    StreamType[StreamType["kMPEG1Audio"] = 3] = "kMPEG1Audio";
    StreamType[StreamType["kMPEG2Audio"] = 4] = "kMPEG2Audio";
    StreamType[StreamType["kPESPrivateData"] = 6] = "kPESPrivateData";
    StreamType[StreamType["kADTSAAC"] = 15] = "kADTSAAC";
    StreamType[StreamType["kLOASAAC"] = 17] = "kLOASAAC";
    StreamType[StreamType["kAC3"] = 129] = "kAC3";
    StreamType[StreamType["kEAC3"] = 135] = "kEAC3";
    StreamType[StreamType["kMetadata"] = 21] = "kMetadata";
    StreamType[StreamType["kSCTE35"] = 134] = "kSCTE35";
    StreamType[StreamType["kPGS"] = 144] = "kPGS";
    StreamType[StreamType["kH264"] = 27] = "kH264";
    StreamType[StreamType["kH265"] = 36] = "kH265";
})(StreamType || (StreamType = {}));
var PMT = /** @class */ (function () {
    function PMT() {
        // pid -> stream_type
        this.pid_stream_type = {};
        this.common_pids = {
            h264: undefined,
            h265: undefined,
            av1: undefined,
            adts_aac: undefined,
            loas_aac: undefined,
            opus: undefined,
            ac3: undefined,
            eac3: undefined,
            mp3: undefined
        };
        this.pes_private_data_pids = {};
        this.timed_id3_pids = {};
        this.pgs_pids = {};
        this.pgs_langs = {};
        this.synchronous_klv_pids = {};
        this.asynchronous_klv_pids = {};
        this.scte_35_pids = {};
        this.smpte2038_pids = {};
    }
    return PMT;
}());

var PESData = /** @class */ (function () {
    function PESData() {
    }
    return PESData;
}());

var SectionData = /** @class */ (function () {
    function SectionData() {
    }
    return SectionData;
}());

var SliceQueue = /** @class */ (function () {
    function SliceQueue() {
        this.slices = [];
        this.total_length = 0;
        this.expected_length = 0;
        this.file_position = 0;
    }
    return SliceQueue;
}());



/***/ }),

/***/ "./src/demux/pes-private-data.ts":
/*!***************************************!*\
  !*** ./src/demux/pes-private-data.ts ***!
  \***************************************/
/*! exports provided: PESPrivateData, PESPrivateDataDescriptor */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "PESPrivateData", function() { return PESPrivateData; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "PESPrivateDataDescriptor", function() { return PESPrivateDataDescriptor; });
// ISO/IEC 13818-1 PES packets containing private data (stream_type=0x06)
var PESPrivateData = /** @class */ (function () {
    function PESPrivateData() {
    }
    return PESPrivateData;
}());

var PESPrivateDataDescriptor = /** @class */ (function () {
    function PESPrivateDataDescriptor() {
    }
    return PESPrivateDataDescriptor;
}());



/***/ }),

/***/ "./src/demux/pgs-data.ts":
/*!*******************************!*\
  !*** ./src/demux/pgs-data.ts ***!
  \*******************************/
/*! exports provided: PGSData */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "PGSData", function() { return PGSData; });
// ISO/IEC 13818-1 PES packets containing private data (stream_type=0x06)
var PGSData = /** @class */ (function () {
    function PGSData() {
    }
    return PGSData;
}());



/***/ }),

/***/ "./src/demux/scte35.ts":
/*!*****************************!*\
  !*** ./src/demux/scte35.ts ***!
  \*****************************/
/*! exports provided: SCTE35CommandType, readSCTE35 */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "SCTE35CommandType", function() { return SCTE35CommandType; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "readSCTE35", function() { return readSCTE35; });
/* harmony import */ var _exp_golomb_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ./exp-golomb.js */ "./src/demux/exp-golomb.js");

var SCTE35CommandType;
(function (SCTE35CommandType) {
    SCTE35CommandType[SCTE35CommandType["kSpliceNull"] = 0] = "kSpliceNull";
    SCTE35CommandType[SCTE35CommandType["kSpliceSchedule"] = 4] = "kSpliceSchedule";
    SCTE35CommandType[SCTE35CommandType["kSpliceInsert"] = 5] = "kSpliceInsert";
    SCTE35CommandType[SCTE35CommandType["kTimeSignal"] = 6] = "kTimeSignal";
    SCTE35CommandType[SCTE35CommandType["kBandwidthReservation"] = 7] = "kBandwidthReservation";
    SCTE35CommandType[SCTE35CommandType["kPrivateCommand"] = 255] = "kPrivateCommand";
})(SCTE35CommandType || (SCTE35CommandType = {}));
var parseSpliceTime = function (reader) {
    var time_specified_flag = reader.readBool();
    if (!time_specified_flag) {
        reader.readBits(7);
        return { time_specified_flag: time_specified_flag };
    }
    else {
        reader.readBits(6);
        var pts_time = reader.readBits(31) * 4 + reader.readBits(2);
        return {
            time_specified_flag: time_specified_flag,
            pts_time: pts_time
        };
    }
};
var parseBreakDuration = function (reader) {
    var auto_return = reader.readBool();
    reader.readBits(6);
    var duration = reader.readBits(31) * 4 + reader.readBits(2);
    return {
        auto_return: auto_return,
        duration: duration
    };
};
var parseSpliceInsertComponent = function (splice_immediate_flag, reader) {
    var component_tag = reader.readBits(8);
    if (splice_immediate_flag) {
        return { component_tag: component_tag };
    }
    var splice_time = parseSpliceTime(reader);
    return {
        component_tag: component_tag,
        splice_time: splice_time
    };
};
var parseSpliceScheduleEventComponent = function (reader) {
    var component_tag = reader.readBits(8);
    var utc_splice_time = reader.readBits(32);
    return {
        component_tag: component_tag,
        utc_splice_time: utc_splice_time
    };
};
var parseSpliceScheduleEvent = function (reader) {
    var splice_event_id = reader.readBits(32);
    var splice_event_cancel_indicator = reader.readBool();
    reader.readBits(7);
    var spliceScheduleEvent = {
        splice_event_id: splice_event_id,
        splice_event_cancel_indicator: splice_event_cancel_indicator
    };
    if (splice_event_cancel_indicator) {
        return spliceScheduleEvent;
    }
    spliceScheduleEvent.out_of_network_indicator = reader.readBool();
    spliceScheduleEvent.program_splice_flag = reader.readBool();
    spliceScheduleEvent.duration_flag = reader.readBool();
    reader.readBits(5);
    if (spliceScheduleEvent.program_splice_flag) {
        spliceScheduleEvent.utc_splice_time = reader.readBits(32);
    }
    else {
        spliceScheduleEvent.component_count = reader.readBits(8);
        spliceScheduleEvent.components = [];
        for (var i = 0; i < spliceScheduleEvent.component_count; i++) {
            spliceScheduleEvent.components.push(parseSpliceScheduleEventComponent(reader));
        }
    }
    if (spliceScheduleEvent.duration_flag) {
        spliceScheduleEvent.break_duration = parseBreakDuration(reader);
    }
    spliceScheduleEvent.unique_program_id = reader.readBits(16);
    spliceScheduleEvent.avail_num = reader.readBits(8);
    spliceScheduleEvent.avails_expected = reader.readBits(8);
    return spliceScheduleEvent;
};
var parseSpliceNull = function () {
    return {};
};
var parseSpliceSchedule = function (reader) {
    var splice_count = reader.readBits(8);
    var events = [];
    for (var i = 0; i < splice_count; i++) {
        events.push(parseSpliceScheduleEvent(reader));
    }
    return {
        splice_count: splice_count,
        events: events
    };
};
var parseSpliceInsert = function (reader) {
    var splice_event_id = reader.readBits(32);
    var splice_event_cancel_indicator = reader.readBool();
    reader.readBits(7);
    var spliceInsert = {
        splice_event_id: splice_event_id,
        splice_event_cancel_indicator: splice_event_cancel_indicator
    };
    if (splice_event_cancel_indicator) {
        return spliceInsert;
    }
    spliceInsert.out_of_network_indicator = reader.readBool();
    spliceInsert.program_splice_flag = reader.readBool();
    spliceInsert.duration_flag = reader.readBool();
    spliceInsert.splice_immediate_flag = reader.readBool();
    reader.readBits(4);
    if (spliceInsert.program_splice_flag && !spliceInsert.splice_immediate_flag) {
        spliceInsert.splice_time = parseSpliceTime(reader);
    }
    if (!spliceInsert.program_splice_flag) {
        spliceInsert.component_count = reader.readBits(8);
        spliceInsert.components = [];
        for (var i = 0; i < spliceInsert.component_count; i++) {
            spliceInsert.components.push(parseSpliceInsertComponent(spliceInsert.splice_immediate_flag, reader));
        }
    }
    if (spliceInsert.duration_flag) {
        spliceInsert.break_duration = parseBreakDuration(reader);
    }
    spliceInsert.unique_program_id = reader.readBits(16);
    spliceInsert.avail_num = reader.readBits(8);
    spliceInsert.avails_expected = reader.readBits(8);
    return spliceInsert;
};
var parseTimeSignal = function (reader) {
    return {
        splice_time: parseSpliceTime(reader)
    };
};
var parseBandwidthReservation = function () {
    return {};
};
var parsePrivateCommand = function (splice_command_length, reader) {
    var identifier = String.fromCharCode(reader.readBits(8), reader.readBits(8), reader.readBits(8), reader.readBits(8));
    var data = new Uint8Array(splice_command_length - 4);
    for (var i = 0; i < splice_command_length - 4; i++) {
        data[i] = reader.readBits(8);
    }
    return {
        identifier: identifier,
        private_data: data.buffer
    };
};
var parseAvailDescriptor = function (descriptor_tag, descriptor_length, identifier, reader) {
    var provider_avail_id = reader.readBits(32);
    return {
        descriptor_tag: descriptor_tag,
        descriptor_length: descriptor_length,
        identifier: identifier,
        provider_avail_id: provider_avail_id
    };
};
var parseDTMFDescriptor = function (descriptor_tag, descriptor_length, identifier, reader) {
    var preroll = reader.readBits(8);
    var dtmf_count = reader.readBits(3);
    reader.readBits(5);
    var DTMF_char = '';
    for (var i = 0; i < dtmf_count; i++) {
        DTMF_char += String.fromCharCode(reader.readBits(8));
    }
    return {
        descriptor_tag: descriptor_tag,
        descriptor_length: descriptor_length,
        identifier: identifier,
        preroll: preroll,
        dtmf_count: dtmf_count,
        DTMF_char: DTMF_char
    };
};
var parseSegmentationDescriptorComponent = function (reader) {
    var component_tag = reader.readBits(8);
    reader.readBits(7);
    var pts_offset = reader.readBits(31) * 4 + reader.readBits(2);
    return {
        component_tag: component_tag,
        pts_offset: pts_offset
    };
};
var parseSegmentationDescriptor = function (descriptor_tag, descriptor_length, identifier, reader) {
    var segmentation_event_id = reader.readBits(32);
    var segmentation_event_cancel_indicator = reader.readBool();
    reader.readBits(7);
    var segmentationDescriptor = {
        descriptor_tag: descriptor_tag,
        descriptor_length: descriptor_length,
        identifier: identifier,
        segmentation_event_id: segmentation_event_id,
        segmentation_event_cancel_indicator: segmentation_event_cancel_indicator
    };
    if (segmentation_event_cancel_indicator) {
        return segmentationDescriptor;
    }
    segmentationDescriptor.program_segmentation_flag = reader.readBool();
    segmentationDescriptor.segmentation_duration_flag = reader.readBool();
    segmentationDescriptor.delivery_not_restricted_flag = reader.readBool();
    if (!segmentationDescriptor.delivery_not_restricted_flag) {
        segmentationDescriptor.web_delivery_allowed_flag = reader.readBool();
        segmentationDescriptor.no_regional_blackout_flag = reader.readBool();
        segmentationDescriptor.archive_allowed_flag = reader.readBool();
        segmentationDescriptor.device_restrictions = reader.readBits(2);
    }
    else {
        reader.readBits(5);
    }
    if (!segmentationDescriptor.program_segmentation_flag) {
        segmentationDescriptor.component_count = reader.readBits(8);
        segmentationDescriptor.components = [];
        for (var i = 0; i < segmentationDescriptor.component_count; i++) {
            segmentationDescriptor.components.push(parseSegmentationDescriptorComponent(reader));
        }
    }
    if (segmentationDescriptor.segmentation_duration_flag) {
        segmentationDescriptor.segmentation_duration = reader.readBits(40);
    }
    segmentationDescriptor.segmentation_upid_type = reader.readBits(8);
    segmentationDescriptor.segmentation_upid_length = reader.readBits(8);
    {
        var upid = new Uint8Array(segmentationDescriptor.segmentation_upid_length);
        for (var i = 0; i < segmentationDescriptor.segmentation_upid_length; i++) {
            upid[i] = reader.readBits(8);
        }
        segmentationDescriptor.segmentation_upid = upid.buffer;
    }
    segmentationDescriptor.segmentation_type_id = reader.readBits(8);
    segmentationDescriptor.segment_num = reader.readBits(8);
    segmentationDescriptor.segments_expected = reader.readBits(8);
    if (segmentationDescriptor.segmentation_type_id === 0x34 ||
        segmentationDescriptor.segmentation_type_id === 0x36 ||
        segmentationDescriptor.segmentation_type_id === 0x38 ||
        segmentationDescriptor.segmentation_type_id === 0x3A) {
        segmentationDescriptor.sub_segment_num = reader.readBits(8);
        segmentationDescriptor.sub_segments_expected = reader.readBits(8);
    }
    return segmentationDescriptor;
};
var parseTimeDescriptor = function (descriptor_tag, descriptor_length, identifier, reader) {
    var TAI_seconds = reader.readBits(48);
    var TAI_ns = reader.readBits(32);
    var UTC_offset = reader.readBits(16);
    return {
        descriptor_tag: descriptor_tag,
        descriptor_length: descriptor_length,
        identifier: identifier,
        TAI_seconds: TAI_seconds,
        TAI_ns: TAI_ns,
        UTC_offset: UTC_offset
    };
};
var parseAudioDescriptorComponent = function (reader) {
    var component_tag = reader.readBits(8);
    var ISO_code = String.fromCharCode(reader.readBits(8), reader.readBits(8), reader.readBits(8));
    var Bit_Stream_Mode = reader.readBits(3);
    var Num_Channels = reader.readBits(4);
    var Full_Srvc_Audio = reader.readBool();
    return {
        component_tag: component_tag,
        ISO_code: ISO_code,
        Bit_Stream_Mode: Bit_Stream_Mode,
        Num_Channels: Num_Channels,
        Full_Srvc_Audio: Full_Srvc_Audio
    };
};
var parseAudioDescriptor = function (descriptor_tag, descriptor_length, identifier, reader) {
    var audio_count = reader.readBits(4);
    var components = [];
    for (var i = 0; i < audio_count; i++) {
        components.push(parseAudioDescriptorComponent(reader));
    }
    return {
        descriptor_tag: descriptor_tag,
        descriptor_length: descriptor_length,
        identifier: identifier,
        audio_count: audio_count,
        components: components
    };
};
var readSCTE35 = function (data) {
    var reader = new _exp_golomb_js__WEBPACK_IMPORTED_MODULE_0__["default"](data);
    var table_id = reader.readBits(8);
    var section_syntax_indicator = reader.readBool();
    var private_indicator = reader.readBool();
    reader.readBits(2);
    var section_length = reader.readBits(12);
    var protocol_version = reader.readBits(8);
    var encrypted_packet = reader.readBool();
    var encryption_algorithm = reader.readBits(6);
    var pts_adjustment = reader.readBits(31) * 4 + reader.readBits(2);
    var cw_index = reader.readBits(8);
    var tier = reader.readBits(12);
    var splice_command_length = reader.readBits(12);
    var splice_command_type = reader.readBits(8);
    var splice_command = null;
    if (splice_command_type === SCTE35CommandType.kSpliceNull) {
        splice_command = parseSpliceNull();
    }
    else if (splice_command_type === SCTE35CommandType.kSpliceSchedule) {
        splice_command = parseSpliceSchedule(reader);
    }
    else if (splice_command_type === SCTE35CommandType.kSpliceInsert) {
        splice_command = parseSpliceInsert(reader);
    }
    else if (splice_command_type === SCTE35CommandType.kTimeSignal) {
        splice_command = parseTimeSignal(reader);
    }
    else if (splice_command_type === SCTE35CommandType.kBandwidthReservation) {
        splice_command = parseBandwidthReservation();
    }
    else if (splice_command_type === SCTE35CommandType.kPrivateCommand) {
        splice_command = parsePrivateCommand(splice_command_length, reader);
    }
    else {
        reader.readBits(splice_command_length * 8);
    }
    var splice_descriptors = [];
    var descriptor_loop_length = reader.readBits(16);
    for (var length_1 = 0; length_1 < descriptor_loop_length;) {
        var descriptor_tag = reader.readBits(8);
        var descriptor_length = reader.readBits(8);
        var identifier = String.fromCharCode(reader.readBits(8), reader.readBits(8), reader.readBits(8), reader.readBits(8));
        if (descriptor_tag === 0x00) {
            splice_descriptors.push(parseAvailDescriptor(descriptor_tag, descriptor_length, identifier, reader));
        }
        else if (descriptor_tag === 0x01) {
            splice_descriptors.push(parseDTMFDescriptor(descriptor_tag, descriptor_length, identifier, reader));
        }
        else if (descriptor_tag === 0x02) {
            splice_descriptors.push(parseSegmentationDescriptor(descriptor_tag, descriptor_length, identifier, reader));
        }
        else if (descriptor_tag === 0x03) {
            splice_descriptors.push(parseTimeDescriptor(descriptor_tag, descriptor_length, identifier, reader));
        }
        else if (descriptor_tag === 0x04) {
            splice_descriptors.push(parseAudioDescriptor(descriptor_tag, descriptor_length, identifier, reader));
        }
        else {
            reader.readBits((descriptor_length - 4) * 8);
        }
        length_1 += 2 + descriptor_length;
    }
    var E_CRC32 = encrypted_packet ? reader.readBits(32) : undefined;
    var CRC32 = reader.readBits(32);
    var detail = {
        table_id: table_id,
        section_syntax_indicator: section_syntax_indicator,
        private_indicator: private_indicator,
        section_length: section_length,
        protocol_version: protocol_version,
        encrypted_packet: encrypted_packet,
        encryption_algorithm: encryption_algorithm,
        pts_adjustment: pts_adjustment,
        cw_index: cw_index,
        tier: tier,
        splice_command_length: splice_command_length,
        splice_command_type: splice_command_type,
        splice_command: splice_command,
        descriptor_loop_length: descriptor_loop_length,
        splice_descriptors: splice_descriptors,
        E_CRC32: E_CRC32,
        CRC32: CRC32
    };
    if (splice_command_type === SCTE35CommandType.kSpliceInsert) {
        var spliceInsert = splice_command;
        if (spliceInsert.splice_event_cancel_indicator) {
            return {
                splice_command_type: splice_command_type,
                detail: detail,
                data: data
            };
        }
        else if (spliceInsert.program_splice_flag && !spliceInsert.splice_immediate_flag) {
            var auto_return = spliceInsert.duration_flag ? spliceInsert.break_duration.auto_return : undefined;
            var duraiton = spliceInsert.duration_flag ? spliceInsert.break_duration.duration / 90 : undefined;
            if (spliceInsert.splice_time.time_specified_flag) {
                return {
                    splice_command_type: splice_command_type,
                    pts: (pts_adjustment + spliceInsert.splice_time.pts_time) % (Math.pow(2, 33)),
                    auto_return: auto_return,
                    duraiton: duraiton,
                    detail: detail,
                    data: data
                };
            }
            else {
                return {
                    splice_command_type: splice_command_type,
                    auto_return: auto_return,
                    duraiton: duraiton,
                    detail: detail,
                    data: data
                };
            }
        }
        else {
            var auto_return = spliceInsert.duration_flag ? spliceInsert.break_duration.auto_return : undefined;
            var duraiton = spliceInsert.duration_flag ? spliceInsert.break_duration.duration / 90 : undefined;
            return {
                splice_command_type: splice_command_type,
                auto_return: auto_return,
                duraiton: duraiton,
                detail: detail,
                data: data
            };
        }
    }
    else if (splice_command_type === SCTE35CommandType.kTimeSignal) {
        var timeSignal = splice_command;
        if (timeSignal.splice_time.time_specified_flag) {
            return {
                splice_command_type: splice_command_type,
                pts: (pts_adjustment + timeSignal.splice_time.pts_time) % (Math.pow(2, 33)),
                detail: detail,
                data: data
            };
        }
        else {
            return {
                splice_command_type: splice_command_type,
                detail: detail,
                data: data
            };
        }
    }
    else {
        return {
            splice_command_type: splice_command_type,
            detail: detail,
            data: data
        };
    }
};


/***/ }),

/***/ "./src/demux/smpte2038.ts":
/*!********************************!*\
  !*** ./src/demux/smpte2038.ts ***!
  \********************************/
/*! exports provided: SMPTE2038Data, smpte2038parse */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "SMPTE2038Data", function() { return SMPTE2038Data; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "smpte2038parse", function() { return smpte2038parse; });
/* harmony import */ var _exp_golomb__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ./exp-golomb */ "./src/demux/exp-golomb.js");

var SMPTE2038Data = /** @class */ (function () {
    function SMPTE2038Data() {
    }
    return SMPTE2038Data;
}());

var smpte2038parse = function (data) {
    var gb = new _exp_golomb__WEBPACK_IMPORTED_MODULE_0__["default"](data);
    var readBits = 0;
    var ancillaries = [];
    while (true) {
        var zero = gb.readBits(6);
        readBits += 6;
        if (zero !== 0) {
            break;
        }
        var YC_indicator = gb.readBool();
        readBits += 1;
        var line_number = gb.readBits(11);
        readBits += 11;
        var horizontal_offset = gb.readBits(12);
        readBits += 12;
        var data_ID = gb.readBits(10) & 0xFF;
        readBits += 10;
        var data_SDID = gb.readBits(10) & 0xFF;
        readBits += 10;
        var data_count = gb.readBits(10) & 0xFF;
        readBits += 10;
        var user_data = new Uint8Array(data_count);
        for (var i = 0; i < data_count; i++) {
            var user_data_word = gb.readBits(10) & 0xFF;
            readBits += 10;
            user_data[i] = user_data_word;
        }
        var checksum_word = gb.readBits(10);
        readBits += 10;
        var description = 'User Defined';
        var information = {};
        if (data_ID === 0x41) {
            if (data_SDID === 0x07) {
                description = 'SCTE-104';
            }
        }
        else if (data_ID === 0x5F) {
            if (data_SDID === 0xDC) {
                description = 'ARIB STD-B37 (1SEG)';
            }
            else if (data_SDID === 0xDD) {
                description = 'ARIB STD-B37 (ANALOG)';
            }
            else if (data_SDID === 0xDE) {
                description = 'ARIB STD-B37 (SD)';
            }
            else if (data_SDID === 0xDF) {
                description = 'ARIB STD-B37 (HD)';
            }
        }
        else if (data_ID === 0x61) {
            if (data_SDID === 0x01) {
                description = 'EIA-708';
            }
            else if (data_SDID === 0x02) {
                description = 'EIA-608';
            }
        }
        ancillaries.push({
            yc_indicator: YC_indicator,
            line_number: line_number,
            horizontal_offset: horizontal_offset,
            did: data_ID,
            sdid: data_SDID,
            user_data: user_data,
            description: description,
            information: information
        });
        gb.readBits(8 - (readBits - Math.floor(readBits / 8)) % 8);
        readBits += (8 - (readBits - Math.floor(readBits / 8))) % 8;
    }
    gb.destroy();
    gb = null;
    return ancillaries;
};


/***/ }),

/***/ "./src/demux/sps-parser.js":
/*!*********************************!*\
  !*** ./src/demux/sps-parser.js ***!
  \*********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _exp_golomb_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ./exp-golomb.js */ "./src/demux/exp-golomb.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

var SPSParser = /** @class */ (function () {
    function SPSParser() {
    }
    SPSParser._ebsp2rbsp = function (uint8array) {
        var src = uint8array;
        var src_length = src.byteLength;
        var dst = new Uint8Array(src_length);
        var dst_idx = 0;
        for (var i = 0; i < src_length; i++) {
            if (i >= 2) {
                // Unescape: Skip 0x03 after 00 00
                if (src[i] === 0x03 && src[i - 1] === 0x00 && src[i - 2] === 0x00) {
                    continue;
                }
            }
            dst[dst_idx] = src[i];
            dst_idx++;
        }
        return new Uint8Array(dst.buffer, 0, dst_idx);
    };
    SPSParser.parseSPS = function (uint8array) {
        var codec_array = uint8array.subarray(1, 4);
        var codec_mimetype = 'avc1.';
        for (var j = 0; j < 3; j++) {
            var h = codec_array[j].toString(16);
            if (h.length < 2) {
                h = '0' + h;
            }
            codec_mimetype += h;
        }
        var rbsp = SPSParser._ebsp2rbsp(uint8array);
        var gb = new _exp_golomb_js__WEBPACK_IMPORTED_MODULE_0__["default"](rbsp);
        gb.readByte();
        var profile_idc = gb.readByte(); // profile_idc
        gb.readByte(); // constraint_set_flags[5] + reserved_zero[3]
        var level_idc = gb.readByte(); // level_idc
        gb.readUEG(); // seq_parameter_set_id
        var profile_string = SPSParser.getProfileString(profile_idc);
        var level_string = SPSParser.getLevelString(level_idc);
        var chroma_format_idc = 1;
        var chroma_format = 420;
        var chroma_format_table = [0, 420, 422, 444];
        var bit_depth_luma = 8;
        var bit_depth_chroma = 8;
        if (profile_idc === 100 || profile_idc === 110 || profile_idc === 122 ||
            profile_idc === 244 || profile_idc === 44 || profile_idc === 83 ||
            profile_idc === 86 || profile_idc === 118 || profile_idc === 128 ||
            profile_idc === 138 || profile_idc === 144) {
            chroma_format_idc = gb.readUEG();
            if (chroma_format_idc === 3) {
                gb.readBits(1); // separate_colour_plane_flag
            }
            if (chroma_format_idc <= 3) {
                chroma_format = chroma_format_table[chroma_format_idc];
            }
            bit_depth_luma = gb.readUEG() + 8; // bit_depth_luma_minus8
            bit_depth_chroma = gb.readUEG() + 8; // bit_depth_chroma_minus8
            gb.readBits(1); // qpprime_y_zero_transform_bypass_flag
            if (gb.readBool()) { // seq_scaling_matrix_present_flag
                var scaling_list_count = (chroma_format_idc !== 3) ? 8 : 12;
                for (var i = 0; i < scaling_list_count; i++) {
                    if (gb.readBool()) { // seq_scaling_list_present_flag
                        if (i < 6) {
                            SPSParser._skipScalingList(gb, 16);
                        }
                        else {
                            SPSParser._skipScalingList(gb, 64);
                        }
                    }
                }
            }
        }
        gb.readUEG(); // log2_max_frame_num_minus4
        var pic_order_cnt_type = gb.readUEG();
        if (pic_order_cnt_type === 0) {
            gb.readUEG(); // log2_max_pic_order_cnt_lsb_minus_4
        }
        else if (pic_order_cnt_type === 1) {
            gb.readBits(1); // delta_pic_order_always_zero_flag
            gb.readSEG(); // offset_for_non_ref_pic
            gb.readSEG(); // offset_for_top_to_bottom_field
            var num_ref_frames_in_pic_order_cnt_cycle = gb.readUEG();
            for (var i = 0; i < num_ref_frames_in_pic_order_cnt_cycle; i++) {
                gb.readSEG(); // offset_for_ref_frame
            }
        }
        var ref_frames = gb.readUEG(); // max_num_ref_frames
        gb.readBits(1); // gaps_in_frame_num_value_allowed_flag
        var pic_width_in_mbs_minus1 = gb.readUEG();
        var pic_height_in_map_units_minus1 = gb.readUEG();
        var frame_mbs_only_flag = gb.readBits(1);
        if (frame_mbs_only_flag === 0) {
            gb.readBits(1); // mb_adaptive_frame_field_flag
        }
        gb.readBits(1); // direct_8x8_inference_flag
        var frame_crop_left_offset = 0;
        var frame_crop_right_offset = 0;
        var frame_crop_top_offset = 0;
        var frame_crop_bottom_offset = 0;
        var frame_cropping_flag = gb.readBool();
        if (frame_cropping_flag) {
            frame_crop_left_offset = gb.readUEG();
            frame_crop_right_offset = gb.readUEG();
            frame_crop_top_offset = gb.readUEG();
            frame_crop_bottom_offset = gb.readUEG();
        }
        var sar_width = 1, sar_height = 1;
        var fps = 0, fps_fixed = true, fps_num = 0, fps_den = 0;
        var vui_parameters_present_flag = gb.readBool();
        if (vui_parameters_present_flag) {
            if (gb.readBool()) { // aspect_ratio_info_present_flag
                var aspect_ratio_idc = gb.readByte();
                var sar_w_table = [1, 12, 10, 16, 40, 24, 20, 32, 80, 18, 15, 64, 160, 4, 3, 2];
                var sar_h_table = [1, 11, 11, 11, 33, 11, 11, 11, 33, 11, 11, 33, 99, 3, 2, 1];
                if (aspect_ratio_idc > 0 && aspect_ratio_idc < 16) {
                    sar_width = sar_w_table[aspect_ratio_idc - 1];
                    sar_height = sar_h_table[aspect_ratio_idc - 1];
                }
                else if (aspect_ratio_idc === 255) {
                    sar_width = gb.readByte() << 8 | gb.readByte();
                    sar_height = gb.readByte() << 8 | gb.readByte();
                }
            }
            if (gb.readBool()) { // overscan_info_present_flag
                gb.readBool(); // overscan_appropriate_flag
            }
            if (gb.readBool()) { // video_signal_type_present_flag
                gb.readBits(4); // video_format & video_full_range_flag
                if (gb.readBool()) { // colour_description_present_flag
                    gb.readBits(24); // colour_primaries & transfer_characteristics & matrix_coefficients
                }
            }
            if (gb.readBool()) { // chroma_loc_info_present_flag
                gb.readUEG(); // chroma_sample_loc_type_top_field
                gb.readUEG(); // chroma_sample_loc_type_bottom_field
            }
            if (gb.readBool()) { // timing_info_present_flag
                var num_units_in_tick = gb.readBits(32);
                var time_scale = gb.readBits(32);
                fps_fixed = gb.readBool(); // fixed_frame_rate_flag
                fps_num = time_scale;
                fps_den = num_units_in_tick * 2;
                fps = fps_num / fps_den;
            }
        }
        var sarScale = 1;
        if (sar_width !== 1 || sar_height !== 1) {
            sarScale = sar_width / sar_height;
        }
        var crop_unit_x = 0, crop_unit_y = 0;
        if (chroma_format_idc === 0) {
            crop_unit_x = 1;
            crop_unit_y = 2 - frame_mbs_only_flag;
        }
        else {
            var sub_wc = (chroma_format_idc === 3) ? 1 : 2;
            var sub_hc = (chroma_format_idc === 1) ? 2 : 1;
            crop_unit_x = sub_wc;
            crop_unit_y = sub_hc * (2 - frame_mbs_only_flag);
        }
        var codec_width = (pic_width_in_mbs_minus1 + 1) * 16;
        var codec_height = (2 - frame_mbs_only_flag) * ((pic_height_in_map_units_minus1 + 1) * 16);
        codec_width -= (frame_crop_left_offset + frame_crop_right_offset) * crop_unit_x;
        codec_height -= (frame_crop_top_offset + frame_crop_bottom_offset) * crop_unit_y;
        var present_width = Math.ceil(codec_width * sarScale);
        gb.destroy();
        gb = null;
        return {
            codec_mimetype: codec_mimetype,
            profile_idc: profile_idc,
            level_idc: level_idc,
            profile_string: profile_string,
            level_string: level_string,
            chroma_format_idc: chroma_format_idc,
            bit_depth: bit_depth_luma,
            bit_depth_luma: bit_depth_luma,
            bit_depth_chroma: bit_depth_chroma,
            ref_frames: ref_frames,
            chroma_format: chroma_format,
            chroma_format_string: SPSParser.getChromaFormatString(chroma_format),
            frame_rate: {
                fixed: fps_fixed,
                fps: fps,
                fps_den: fps_den,
                fps_num: fps_num
            },
            sar_ratio: {
                width: sar_width,
                height: sar_height
            },
            codec_size: {
                width: codec_width,
                height: codec_height
            },
            present_size: {
                width: present_width,
                height: codec_height
            }
        };
    };
    SPSParser._skipScalingList = function (gb, count) {
        var last_scale = 8, next_scale = 8;
        var delta_scale = 0;
        for (var i = 0; i < count; i++) {
            if (next_scale !== 0) {
                delta_scale = gb.readSEG();
                next_scale = (last_scale + delta_scale + 256) % 256;
            }
            last_scale = (next_scale === 0) ? last_scale : next_scale;
        }
    };
    SPSParser.getProfileString = function (profile_idc) {
        switch (profile_idc) {
            case 66:
                return 'Baseline';
            case 77:
                return 'Main';
            case 88:
                return 'Extended';
            case 100:
                return 'High';
            case 110:
                return 'High10';
            case 122:
                return 'High422';
            case 244:
                return 'High444';
            default:
                return 'Unknown';
        }
    };
    SPSParser.getLevelString = function (level_idc) {
        return (level_idc / 10).toFixed(1);
    };
    SPSParser.getChromaFormatString = function (chroma) {
        switch (chroma) {
            case 420:
                return '4:2:0';
            case 422:
                return '4:2:2';
            case 444:
                return '4:4:4';
            default:
                return 'Unknown';
        }
    };
    return SPSParser;
}());
/* harmony default export */ __webpack_exports__["default"] = (SPSParser);


/***/ }),

/***/ "./src/demux/ts-demuxer.ts":
/*!*********************************!*\
  !*** ./src/demux/ts-demuxer.ts ***!
  \*********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger */ "./src/utils/logger.js");
/* harmony import */ var _core_media_info__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ../core/media-info */ "./src/core/media-info.js");
/* harmony import */ var _utils_exception__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ../utils/exception */ "./src/utils/exception.js");
/* harmony import */ var _base_demuxer__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ./base-demuxer */ "./src/demux/base-demuxer.ts");
/* harmony import */ var _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__ = __webpack_require__(/*! ./pat-pmt-pes */ "./src/demux/pat-pmt-pes.ts");
/* harmony import */ var _h264__WEBPACK_IMPORTED_MODULE_5__ = __webpack_require__(/*! ./h264 */ "./src/demux/h264.ts");
/* harmony import */ var _sps_parser__WEBPACK_IMPORTED_MODULE_6__ = __webpack_require__(/*! ./sps-parser */ "./src/demux/sps-parser.js");
/* harmony import */ var _aac__WEBPACK_IMPORTED_MODULE_7__ = __webpack_require__(/*! ./aac */ "./src/demux/aac.ts");
/* harmony import */ var _pes_private_data__WEBPACK_IMPORTED_MODULE_8__ = __webpack_require__(/*! ./pes-private-data */ "./src/demux/pes-private-data.ts");
/* harmony import */ var _scte35__WEBPACK_IMPORTED_MODULE_9__ = __webpack_require__(/*! ./scte35 */ "./src/demux/scte35.ts");
/* harmony import */ var _h265__WEBPACK_IMPORTED_MODULE_10__ = __webpack_require__(/*! ./h265 */ "./src/demux/h265.ts");
/* harmony import */ var _h265_parser__WEBPACK_IMPORTED_MODULE_11__ = __webpack_require__(/*! ./h265-parser */ "./src/demux/h265-parser.js");
/* harmony import */ var _smpte2038__WEBPACK_IMPORTED_MODULE_12__ = __webpack_require__(/*! ./smpte2038 */ "./src/demux/smpte2038.ts");
/* harmony import */ var _mp3__WEBPACK_IMPORTED_MODULE_13__ = __webpack_require__(/*! ./mp3 */ "./src/demux/mp3.ts");
/* harmony import */ var _ac3__WEBPACK_IMPORTED_MODULE_14__ = __webpack_require__(/*! ./ac3 */ "./src/demux/ac3.ts");
/* harmony import */ var _klv__WEBPACK_IMPORTED_MODULE_15__ = __webpack_require__(/*! ./klv */ "./src/demux/klv.ts");
/* harmony import */ var _av1__WEBPACK_IMPORTED_MODULE_16__ = __webpack_require__(/*! ./av1 */ "./src/demux/av1.ts");
/* harmony import */ var _av1_parser__WEBPACK_IMPORTED_MODULE_17__ = __webpack_require__(/*! ./av1-parser */ "./src/demux/av1-parser.ts");
/* harmony import */ var _pgs_data__WEBPACK_IMPORTED_MODULE_18__ = __webpack_require__(/*! ./pgs-data */ "./src/demux/pgs-data.ts");
/*
 * Copyright (C) 2021 magicxqq. All Rights Reserved.
 *
 * @author magicxqq <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var __extends = (undefined && undefined.__extends) || (function () {
    var extendStatics = function (d, b) {
        extendStatics = Object.setPrototypeOf ||
            ({ __proto__: [] } instanceof Array && function (d, b) { d.__proto__ = b; }) ||
            function (d, b) { for (var p in b) if (Object.prototype.hasOwnProperty.call(b, p)) d[p] = b[p]; };
        return extendStatics(d, b);
    };
    return function (d, b) {
        if (typeof b !== "function" && b !== null)
            throw new TypeError("Class extends value " + String(b) + " is not a constructor or null");
        extendStatics(d, b);
        function __() { this.constructor = d; }
        d.prototype = b === null ? Object.create(b) : (__.prototype = b.prototype, new __());
    };
})();
var __assign = (undefined && undefined.__assign) || function () {
    __assign = Object.assign || function(t) {
        for (var s, i = 1, n = arguments.length; i < n; i++) {
            s = arguments[i];
            for (var p in s) if (Object.prototype.hasOwnProperty.call(s, p))
                t[p] = s[p];
        }
        return t;
    };
    return __assign.apply(this, arguments);
};



















var TSDemuxer = /** @class */ (function (_super) {
    __extends(TSDemuxer, _super);
    function TSDemuxer(probe_data, config) {
        var _this = _super.call(this) || this;
        _this.TAG = 'TSDemuxer';
        _this.first_parse_ = true;
        _this.media_info_ = new _core_media_info__WEBPACK_IMPORTED_MODULE_1__["default"]();
        _this.timescale_ = 90;
        _this.duration_ = 0;
        _this.current_pmt_pid_ = -1;
        _this.program_pmt_map_ = {};
        _this.pes_slice_queues_ = {};
        _this.section_slice_queues_ = {};
        _this.video_metadata_ = {
            vps: undefined,
            sps: undefined,
            pps: undefined,
            av1c: undefined,
            details: undefined
        };
        _this.audio_metadata_ = {
            codec: undefined,
            audio_object_type: undefined,
            sampling_freq_index: undefined,
            sampling_frequency: undefined,
            channel_config: undefined
        };
        _this.last_pcr_base_ = NaN;
        _this.timestamp_offset_ = 0;
        _this.audio_last_sample_pts_ = undefined;
        _this.aac_last_incomplete_data_ = null;
        _this.has_video_ = false;
        _this.has_audio_ = false;
        _this.video_init_segment_dispatched_ = false;
        _this.audio_init_segment_dispatched_ = false;
        _this.video_metadata_changed_ = false;
        _this.audio_metadata_changed_ = false;
        _this.loas_previous_frame = null;
        _this.video_track_ = { type: 'video', id: 1, sequenceNumber: 0, samples: [], length: 0 };
        _this.audio_track_ = { type: 'audio', id: 2, sequenceNumber: 0, samples: [], length: 0 };
        _this.ts_packet_size_ = probe_data.ts_packet_size;
        _this.sync_offset_ = probe_data.sync_offset;
        _this.config_ = config;
        return _this;
    }
    TSDemuxer.prototype.destroy = function () {
        this.media_info_ = null;
        this.pes_slice_queues_ = null;
        this.section_slice_queues_ = null;
        this.video_metadata_ = null;
        this.audio_metadata_ = null;
        this.aac_last_incomplete_data_ = null;
        this.video_track_ = null;
        this.audio_track_ = null;
        _super.prototype.destroy.call(this);
    };
    TSDemuxer.probe = function (buffer) {
        var data = new Uint8Array(buffer);
        var sync_offset = -1;
        var ts_packet_size = 188;
        if (data.byteLength <= 3 * ts_packet_size) {
            return { needMoreData: true };
        }
        while (sync_offset === -1) {
            var scan_window = Math.min(1000, data.byteLength - 3 * ts_packet_size);
            for (var i = 0; i < scan_window;) {
                // sync_byte should all be 0x47
                if (data[i] === 0x47
                    && data[i + ts_packet_size] === 0x47
                    && data[i + 2 * ts_packet_size] === 0x47) {
                    sync_offset = i;
                    break;
                }
                else {
                    i++;
                }
            }
            // find sync_offset failed in previous ts_packet_size
            if (sync_offset === -1) {
                if (ts_packet_size === 188) {
                    // try 192 packet size (BDAV, etc.)
                    ts_packet_size = 192;
                }
                else if (ts_packet_size === 192) {
                    // try 204 packet size (European DVB, etc.)
                    ts_packet_size = 204;
                }
                else {
                    // 192, 204 also failed, exit
                    break;
                }
            }
        }
        if (sync_offset === -1) {
            // both 188, 192, 204 failed, Non MPEG-TS
            return { match: false };
        }
        if (ts_packet_size === 192 && sync_offset >= 4) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v('TSDemuxer', "ts_packet_size = 192, m2ts mode");
            sync_offset -= 4;
        }
        else if (ts_packet_size === 204) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v('TSDemuxer', "ts_packet_size = 204, RS encoded MPEG2-TS stream");
        }
        return {
            match: true,
            consumed: 0,
            ts_packet_size: ts_packet_size,
            sync_offset: sync_offset
        };
    };
    TSDemuxer.prototype.bindDataSource = function (loader) {
        loader.onDataArrival = this.parseChunks.bind(this);
        return this;
    };
    TSDemuxer.prototype.resetMediaInfo = function () {
        this.media_info_ = new _core_media_info__WEBPACK_IMPORTED_MODULE_1__["default"]();
    };
    TSDemuxer.prototype.parseChunks = function (chunk, byte_start) {
        if (!this.onError
            || !this.onMediaInfo
            || !this.onTrackMetadata
            || !this.onDataAvailable) {
            throw new _utils_exception__WEBPACK_IMPORTED_MODULE_2__["IllegalStateException"]('onError & onMediaInfo & onTrackMetadata & onDataAvailable callback must be specified');
        }
        var offset = 0;
        if (this.first_parse_) {
            this.first_parse_ = false;
            offset = this.sync_offset_;
        }
        while (offset + this.ts_packet_size_ <= chunk.byteLength) {
            var file_position = byte_start + offset;
            if (this.ts_packet_size_ === 192) {
                // skip ATS field (2-bits copy-control + 30-bits timestamp) for m2ts
                offset += 4;
            }
            var data = new Uint8Array(chunk, offset, 188);
            var sync_byte = data[0];
            if (sync_byte !== 0x47) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "sync_byte = ".concat(sync_byte, ", not 0x47"));
                break;
            }
            var payload_unit_start_indicator = (data[1] & 0x40) >>> 6;
            var transport_priority = (data[1] & 0x20) >>> 5;
            var pid = ((data[1] & 0x1F) << 8) | data[2];
            var adaptation_field_control = (data[3] & 0x30) >>> 4;
            var continuity_conunter = (data[3] & 0x0F);
            var is_pcr_pid = (this.pmt_ && this.pmt_.pcr_pid === pid) ? true : false;
            var adaptation_field_info = {};
            var ts_payload_start_index = 4;
            if (adaptation_field_control == 0x02 || adaptation_field_control == 0x03) {
                // Adaptation field exists along with / without payload
                var adaptation_field_length = data[4];
                if (adaptation_field_length > 0 && (is_pcr_pid || adaptation_field_control == 0x03)) {
                    // Parse adaptation field
                    adaptation_field_info.discontinuity_indicator = (data[5] & 0x80) >>> 7;
                    adaptation_field_info.random_access_indicator = (data[5] & 0x40) >>> 6;
                    adaptation_field_info.elementary_stream_priority_indicator = (data[5] & 0x20) >>> 5;
                    var PCR_flag = (data[5] & 0x10) >>> 4;
                    if (PCR_flag) {
                        var pcr_base = this.getPcrBase(data);
                        var pcr_extension = ((data[10] & 0x01) << 8) | data[11];
                        var pcr = pcr_base * 300 + pcr_extension;
                        this.last_pcr_ = pcr;
                    }
                }
                if (adaptation_field_control == 0x02 || 5 + adaptation_field_length === 188) {
                    // TS packet only has adaption field, jump to next
                    offset += 188;
                    if (this.ts_packet_size_ === 204) {
                        // skip parity word (16 bytes) for RS encoded TS
                        offset += 16;
                    }
                    continue;
                }
                else {
                    // Point ts_payload_start_index to the start of payload
                    ts_payload_start_index = 4 + 1 + adaptation_field_length;
                }
            }
            if (adaptation_field_control == 0x01 || adaptation_field_control == 0x03) {
                if (pid === 0 || // PAT (pid === 0)
                    pid === this.current_pmt_pid_ || // PMT
                    (this.pmt_ != undefined && this.pmt_.pid_stream_type[pid] === _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kSCTE35)) { // SCTE35
                    var ts_payload_length = 188 - ts_payload_start_index;
                    this.handleSectionSlice(chunk, offset + ts_payload_start_index, ts_payload_length, {
                        pid: pid,
                        file_position: file_position,
                        payload_unit_start_indicator: payload_unit_start_indicator,
                        continuity_conunter: continuity_conunter,
                        random_access_indicator: adaptation_field_info.random_access_indicator
                    });
                }
                else if (this.pmt_ != undefined && this.pmt_.pid_stream_type[pid] != undefined) {
                    // PES
                    var ts_payload_length = 188 - ts_payload_start_index;
                    var stream_type = this.pmt_.pid_stream_type[pid];
                    // process PES only for known common_pids
                    if (pid === this.pmt_.common_pids.h264
                        || pid === this.pmt_.common_pids.h265
                        || pid === this.pmt_.common_pids.av1
                        || pid === this.pmt_.common_pids.adts_aac
                        || pid === this.pmt_.common_pids.loas_aac
                        || pid === this.pmt_.common_pids.ac3
                        || pid === this.pmt_.common_pids.eac3
                        || pid === this.pmt_.common_pids.opus
                        || pid === this.pmt_.common_pids.mp3
                        || this.pmt_.pes_private_data_pids[pid] === true
                        || this.pmt_.timed_id3_pids[pid] === true
                        || this.pmt_.pgs_pids[pid] === true
                        || this.pmt_.synchronous_klv_pids[pid] === true
                        || this.pmt_.asynchronous_klv_pids[pid] === true) {
                        this.handlePESSlice(chunk, offset + ts_payload_start_index, ts_payload_length, {
                            pid: pid,
                            stream_type: stream_type,
                            file_position: file_position,
                            payload_unit_start_indicator: payload_unit_start_indicator,
                            continuity_conunter: continuity_conunter,
                            random_access_indicator: adaptation_field_info.random_access_indicator
                        });
                    }
                }
            }
            offset += 188;
            if (this.ts_packet_size_ === 204) {
                // skip parity word (16 bytes) for RS encoded TS
                offset += 16;
            }
        }
        // dispatch parsed frames to the remuxer (consumer)
        this.dispatchAudioVideoMediaSegment();
        return offset; // consumed bytes
    };
    TSDemuxer.prototype.handleSectionSlice = function (buffer, offset, length, misc) {
        var data = new Uint8Array(buffer, offset, length);
        var slice_queue = this.section_slice_queues_[misc.pid];
        if (misc.payload_unit_start_indicator) {
            var pointer_field = data[0];
            if (slice_queue != undefined && slice_queue.total_length !== 0) {
                var remain_section = new Uint8Array(buffer, offset + 1, Math.min(length, pointer_field));
                slice_queue.slices.push(remain_section);
                slice_queue.total_length += remain_section.byteLength;
                if (slice_queue.total_length === slice_queue.expected_length) {
                    this.emitSectionSlices(slice_queue, misc);
                }
                else {
                    this.clearSlices(slice_queue, misc);
                }
            }
            for (var i = 1 + pointer_field; i < data.byteLength;) {
                var table_id = data[i + 0];
                if (table_id === 0xFF) {
                    break;
                }
                var section_length = ((data[i + 1] & 0x0F) << 8) | data[i + 2];
                this.section_slice_queues_[misc.pid] = new _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["SliceQueue"]();
                slice_queue = this.section_slice_queues_[misc.pid];
                slice_queue.expected_length = section_length + 3;
                slice_queue.file_position = misc.file_position;
                slice_queue.random_access_indicator = misc.random_access_indicator;
                var remain_section = new Uint8Array(buffer, offset + i, Math.min(length - i, slice_queue.expected_length - slice_queue.total_length));
                slice_queue.slices.push(remain_section);
                slice_queue.total_length += remain_section.byteLength;
                if (slice_queue.total_length === slice_queue.expected_length) {
                    this.emitSectionSlices(slice_queue, misc);
                }
                else if (slice_queue.total_length >= slice_queue.expected_length) {
                    this.clearSlices(slice_queue, misc);
                }
                i += remain_section.byteLength;
            }
        }
        else if (slice_queue != undefined && slice_queue.total_length !== 0) {
            var remain_section = new Uint8Array(buffer, offset, Math.min(length, slice_queue.expected_length - slice_queue.total_length));
            slice_queue.slices.push(remain_section);
            slice_queue.total_length += remain_section.byteLength;
            if (slice_queue.total_length === slice_queue.expected_length) {
                this.emitSectionSlices(slice_queue, misc);
            }
            else if (slice_queue.total_length >= slice_queue.expected_length) {
                this.clearSlices(slice_queue, misc);
            }
        }
    };
    TSDemuxer.prototype.handlePESSlice = function (buffer, offset, length, misc) {
        var data = new Uint8Array(buffer, offset, length);
        var packet_start_code_prefix = (data[0] << 16) | (data[1] << 8) | (data[2]);
        var stream_id = data[3];
        var PES_packet_length = (data[4] << 8) | data[5];
        if (misc.payload_unit_start_indicator) {
            if (packet_start_code_prefix !== 1) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "handlePESSlice: packet_start_code_prefix should be 1 but with value ".concat(packet_start_code_prefix));
                return;
            }
            // handle queued PES slices:
            // Merge into a big Uint8Array then call parsePES()
            var slice_queue_1 = this.pes_slice_queues_[misc.pid];
            if (slice_queue_1) {
                if (slice_queue_1.expected_length === 0 || slice_queue_1.expected_length === slice_queue_1.total_length) {
                    this.emitPESSlices(slice_queue_1, misc);
                }
                else {
                    this.clearSlices(slice_queue_1, misc);
                }
            }
            // Make a new PES queue for new PES slices
            this.pes_slice_queues_[misc.pid] = new _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["SliceQueue"]();
            this.pes_slice_queues_[misc.pid].file_position = misc.file_position;
            this.pes_slice_queues_[misc.pid].random_access_indicator = misc.random_access_indicator;
        }
        if (this.pes_slice_queues_[misc.pid] == undefined) {
            // ignore PES slices without [PES slice that has payload_unit_start_indicator]
            return;
        }
        // push subsequent PES slices into pes_queue
        var slice_queue = this.pes_slice_queues_[misc.pid];
        slice_queue.slices.push(data);
        if (misc.payload_unit_start_indicator) {
            slice_queue.expected_length = PES_packet_length === 0 ? 0 : PES_packet_length + 6;
        }
        slice_queue.total_length += data.byteLength;
        if (slice_queue.expected_length > 0 && slice_queue.expected_length === slice_queue.total_length) {
            this.emitPESSlices(slice_queue, misc);
        }
        else if (slice_queue.expected_length > 0 && slice_queue.expected_length < slice_queue.total_length) {
            this.clearSlices(slice_queue, misc);
        }
    };
    TSDemuxer.prototype.emitSectionSlices = function (slice_queue, misc) {
        var data = new Uint8Array(slice_queue.total_length);
        for (var i = 0, offset = 0; i < slice_queue.slices.length; i++) {
            var slice = slice_queue.slices[i];
            data.set(slice, offset);
            offset += slice.byteLength;
        }
        slice_queue.slices = [];
        slice_queue.expected_length = -1;
        slice_queue.total_length = 0;
        var section_data = new _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["SectionData"]();
        section_data.pid = misc.pid;
        section_data.data = data;
        section_data.file_position = slice_queue.file_position;
        section_data.random_access_indicator = slice_queue.random_access_indicator;
        this.parseSection(section_data);
    };
    TSDemuxer.prototype.emitPESSlices = function (slice_queue, misc) {
        var data = new Uint8Array(slice_queue.total_length);
        for (var i = 0, offset = 0; i < slice_queue.slices.length; i++) {
            var slice = slice_queue.slices[i];
            data.set(slice, offset);
            offset += slice.byteLength;
        }
        slice_queue.slices = [];
        slice_queue.expected_length = -1;
        slice_queue.total_length = 0;
        var pes_data = new _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["PESData"]();
        pes_data.pid = misc.pid;
        pes_data.data = data;
        pes_data.stream_type = misc.stream_type;
        pes_data.file_position = slice_queue.file_position;
        pes_data.random_access_indicator = slice_queue.random_access_indicator;
        this.parsePES(pes_data);
    };
    TSDemuxer.prototype.clearSlices = function (slice_queue, misc) {
        slice_queue.slices = [];
        slice_queue.expected_length = -1;
        slice_queue.total_length = 0;
    };
    TSDemuxer.prototype.parseSection = function (section_data) {
        var data = section_data.data;
        var pid = section_data.pid;
        if (pid === 0x00) {
            this.parsePAT(data);
        }
        else if (pid === this.current_pmt_pid_) {
            this.parsePMT(data);
        }
        else if (this.pmt_ != undefined && this.pmt_.scte_35_pids[pid]) {
            this.parseSCTE35(data);
        }
    };
    TSDemuxer.prototype.parsePES = function (pes_data) {
        var data = pes_data.data;
        var packet_start_code_prefix = (data[0] << 16) | (data[1] << 8) | (data[2]);
        var stream_id = data[3];
        var PES_packet_length = (data[4] << 8) | data[5];
        if (packet_start_code_prefix !== 1) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "parsePES: packet_start_code_prefix should be 1 but with value ".concat(packet_start_code_prefix));
            return;
        }
        if (stream_id !== 0xBC // program_stream_map
            && stream_id !== 0xBE // padding_stream
            && stream_id !== 0xBF // private_stream_2
            && stream_id !== 0xF0 // ECM
            && stream_id !== 0xF1 // EMM
            && stream_id !== 0xFF // program_stream_directory
            && stream_id !== 0xF2 // DSMCC
            && stream_id !== 0xF8) {
            var PES_scrambling_control = (data[6] & 0x30) >>> 4;
            var PTS_DTS_flags = (data[7] & 0xC0) >>> 6;
            var PES_header_data_length = data[8];
            var pts = void 0;
            var dts = void 0;
            if (PTS_DTS_flags === 0x02 || PTS_DTS_flags === 0x03) {
                pts = this.getTimestamp(data, 9);
                dts = PTS_DTS_flags === 0x03 ? this.getTimestamp(data, 14) : pts;
            }
            var payload_start_index = 6 + 3 + PES_header_data_length;
            var payload_length = void 0;
            if (PES_packet_length !== 0) {
                if (PES_packet_length < 3 + PES_header_data_length) {
                    _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Malformed PES: PES_packet_length < 3 + PES_header_data_length");
                    return;
                }
                payload_length = PES_packet_length - 3 - PES_header_data_length;
            }
            else { // PES_packet_length === 0
                payload_length = data.byteLength - payload_start_index;
            }
            var payload = data.subarray(payload_start_index, payload_start_index + payload_length);
            switch (pes_data.stream_type) {
                case _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kMPEG1Audio:
                case _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kMPEG2Audio:
                    this.parseMP3Payload(payload, pts);
                    break;
                case _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kPESPrivateData:
                    if (this.pmt_.common_pids.av1 === pes_data.pid) {
                        this.parseAV1Payload(payload, pts, dts, pes_data.file_position, pes_data.random_access_indicator);
                    }
                    else if (this.pmt_.common_pids.opus === pes_data.pid) {
                        this.parseOpusPayload(payload, pts);
                    }
                    else if (this.pmt_.common_pids.ac3 === pes_data.pid) {
                        this.parseAC3Payload(payload, pts);
                    }
                    else if (this.pmt_.common_pids.eac3 === pes_data.pid) {
                        this.parseEAC3Payload(payload, pts);
                    }
                    else if (this.pmt_.asynchronous_klv_pids[pes_data.pid]) {
                        this.parseAsynchronousKLVMetadataPayload(payload, pes_data.pid, stream_id);
                    }
                    else if (this.pmt_.smpte2038_pids[pes_data.pid]) {
                        this.parseSMPTE2038MetadataPayload(payload, pts, dts, pes_data.pid, stream_id);
                    }
                    else {
                        this.parsePESPrivateDataPayload(payload, pts, dts, pes_data.pid, stream_id);
                    }
                    break;
                case _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kADTSAAC:
                    this.parseADTSAACPayload(payload, pts);
                    break;
                case _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kLOASAAC:
                    this.parseLOASAACPayload(payload, pts);
                    break;
                case _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kAC3:
                    this.parseAC3Payload(payload, pts);
                    break;
                case _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kEAC3:
                    this.parseEAC3Payload(payload, pts);
                    break;
                case _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kMetadata:
                    if (this.pmt_.timed_id3_pids[pes_data.pid]) {
                        this.parseTimedID3MetadataPayload(payload, pts, dts, pes_data.pid, stream_id);
                    }
                    else if (this.pmt_.synchronous_klv_pids[pes_data.pid]) {
                        this.parseSynchronousKLVMetadataPayload(payload, pts, dts, pes_data.pid, stream_id);
                    }
                    break;
                case _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kPGS:
                    this.parsePGSPayload(payload, pts, dts, pes_data.pid, stream_id, this.pmt_.pgs_langs[pes_data.pid]);
                    break;
                case _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kH264:
                    this.parseH264Payload(payload, pts, dts, pes_data.file_position, pes_data.random_access_indicator);
                    break;
                case _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kH265:
                    this.parseH265Payload(payload, pts, dts, pes_data.file_position, pes_data.random_access_indicator);
                    break;
                default:
                    break;
            }
        }
        else if (stream_id === 0xBC // program_stream_map
            || stream_id === 0xBF // private_stream_2
            || stream_id === 0xF0 // ECM
            || stream_id === 0xF1 // EMM
            || stream_id === 0xFF // program_stream_directory
            || stream_id === 0xF2 // DSMCC_stream
            || stream_id === 0xF8) { // ITU-T Rec. H.222.1 type E stream
            if (pes_data.stream_type === _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kPESPrivateData) {
                var payload_start_index = 6;
                var payload_length = void 0;
                if (PES_packet_length !== 0) {
                    payload_length = PES_packet_length;
                }
                else { // PES_packet_length === 0
                    payload_length = data.byteLength - payload_start_index;
                }
                var payload = data.subarray(payload_start_index, payload_start_index + payload_length);
                this.parsePESPrivateDataPayload(payload, undefined, undefined, pes_data.pid, stream_id);
            }
        }
    };
    TSDemuxer.prototype.parsePAT = function (data) {
        var table_id = data[0];
        if (table_id !== 0x00) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "parsePAT: table_id ".concat(table_id, " is not corresponded to PAT!"));
            return;
        }
        var section_length = ((data[1] & 0x0F) << 8) | data[2];
        var transport_stream_id = (data[3] << 8) | data[4];
        var version_number = (data[5] & 0x3E) >>> 1;
        var current_next_indicator = data[5] & 0x01;
        var section_number = data[6];
        var last_section_number = data[7];
        var pat = null;
        if (current_next_indicator === 1 && section_number === 0) {
            pat = new _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["PAT"]();
            pat.version_number = version_number;
        }
        else {
            pat = this.pat_;
            if (pat == undefined) {
                return;
            }
        }
        var program_start_index = 8;
        var program_bytes = section_length - 5 - 4; // section_length - (headers + crc)
        var first_program_number = -1;
        var first_pmt_pid = -1;
        for (var i = program_start_index; i < program_start_index + program_bytes; i += 4) {
            var program_number = (data[i] << 8) | data[i + 1];
            var pid = ((data[i + 2] & 0x1F) << 8) | data[i + 3];
            if (program_number === 0) {
                // network_PID
                pat.network_pid = pid;
            }
            else {
                // program_map_PID
                pat.program_pmt_pid[program_number] = pid;
                if (first_program_number === -1) {
                    first_program_number = program_number;
                }
                if (first_pmt_pid === -1) {
                    first_pmt_pid = pid;
                }
            }
        }
        // Currently we only deal with first appeared PMT pid
        if (current_next_indicator === 1 && section_number === 0) {
            if (this.pat_ == undefined) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Parsed first PAT: ".concat(JSON.stringify(pat)));
            }
            this.pat_ = pat;
            this.current_program_ = first_program_number;
            this.current_pmt_pid_ = first_pmt_pid;
        }
    };
    TSDemuxer.prototype.parsePMT = function (data) {
        var table_id = data[0];
        if (table_id !== 0x02) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "parsePMT: table_id ".concat(table_id, " is not corresponded to PMT!"));
            return;
        }
        var section_length = ((data[1] & 0x0F) << 8) | data[2];
        var program_number = (data[3] << 8) | data[4];
        var version_number = (data[5] & 0x3E) >>> 1;
        var current_next_indicator = data[5] & 0x01;
        var section_number = data[6];
        var last_section_number = data[7];
        var pmt = null;
        if (current_next_indicator === 1 && section_number === 0) {
            pmt = new _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["PMT"]();
            pmt.program_number = program_number;
            pmt.version_number = version_number;
            this.program_pmt_map_[program_number] = pmt;
        }
        else {
            pmt = this.program_pmt_map_[program_number];
            if (pmt == undefined) {
                return;
            }
        }
        pmt.pcr_pid = ((data[8] & 0x1F) << 8) | data[9];
        var program_info_length = ((data[10] & 0x0F) << 8) | data[11];
        var info_start_index = 12 + program_info_length;
        var info_bytes = section_length - 9 - program_info_length - 4;
        for (var i = info_start_index; i < info_start_index + info_bytes;) {
            var stream_type = data[i];
            var elementary_PID = ((data[i + 1] & 0x1F) << 8) | data[i + 2];
            var ES_info_length = ((data[i + 3] & 0x0F) << 8) | data[i + 4];
            pmt.pid_stream_type[elementary_PID] = stream_type;
            var already_has_video = pmt.common_pids.h264 || pmt.common_pids.h265;
            var already_has_audio = pmt.common_pids.adts_aac || pmt.common_pids.loas_aac || pmt.common_pids.ac3 || pmt.common_pids.eac3 || pmt.common_pids.opus || pmt.common_pids.mp3;
            if (stream_type === _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kH264 && !already_has_video) {
                pmt.common_pids.h264 = elementary_PID;
            }
            else if (stream_type === _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kH265 && !already_has_video) {
                pmt.common_pids.h265 = elementary_PID;
            }
            else if (stream_type === _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kADTSAAC && !already_has_audio) {
                pmt.common_pids.adts_aac = elementary_PID;
            }
            else if (stream_type === _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kLOASAAC && !already_has_audio) {
                pmt.common_pids.loas_aac = elementary_PID;
            }
            else if (stream_type === _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kAC3 && !already_has_audio) {
                pmt.common_pids.ac3 = elementary_PID; // ATSC AC-3
            }
            else if (stream_type === _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kEAC3 && !already_has_audio) {
                pmt.common_pids.eac3 = elementary_PID; // ATSC EAC-3
            }
            else if ((stream_type === _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kMPEG1Audio || stream_type === _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kMPEG2Audio) && !already_has_audio) {
                pmt.common_pids.mp3 = elementary_PID;
            }
            else if (stream_type === _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kPESPrivateData) {
                pmt.pes_private_data_pids[elementary_PID] = true;
                if (ES_info_length > 0) {
                    // parse descriptor for PES private data
                    for (var offset = i + 5; offset < i + 5 + ES_info_length;) {
                        var tag = data[offset + 0];
                        var length_1 = data[offset + 1];
                        if (tag === 0x05) { // Registration Descriptor
                            var registration = String.fromCharCode.apply(String, Array.from(data.subarray(offset + 2, offset + 2 + length_1)));
                            if (registration === 'VANC') {
                                pmt.smpte2038_pids[elementary_PID] = true;
                            } /* else if (registration === 'AC-3' && !already_has_audio) {
                                pmt.common_pids.ac3 = elementary_PID; // DVB AC-3 (FIXME: NEED VERIFY)
                            } */ /* else if (registration === 'EC-3' && !alrady_has_audio) {
                                pmt.common_pids.eac3 = elementary_PID; // DVB EAC-3 (FIXME: NEED VERIFY)
                            } */
                            else if (registration === 'AV01') {
                                pmt.common_pids.av1 = elementary_PID;
                            }
                            else if (registration === 'Opus') {
                                pmt.common_pids.opus = elementary_PID;
                            }
                            else if (registration === 'KLVA') {
                                pmt.asynchronous_klv_pids[elementary_PID] = true;
                            }
                        }
                        else if (tag === 0x7F) { // DVB extension descriptor
                            if (elementary_PID === pmt.common_pids.opus) {
                                var ext_desc_tag = data[offset + 2];
                                var channel_config_code = null;
                                if (ext_desc_tag === 0x80) { // User defined (provisional Opus)
                                    channel_config_code = data[offset + 3];
                                }
                                if (channel_config_code == null) {
                                    _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "Not Supported Opus channel count.");
                                    continue;
                                }
                                var meta = {
                                    codec: 'opus',
                                    channel_count: (channel_config_code & 0x0F) === 0 ? 2 : (channel_config_code & 0x0F),
                                    channel_config_code: channel_config_code,
                                    sample_rate: 48000
                                };
                                var sample = {
                                    codec: 'opus',
                                    meta: meta
                                };
                                if (this.audio_init_segment_dispatched_ == false) {
                                    this.audio_metadata_ = meta;
                                    this.dispatchAudioInitSegment(sample);
                                }
                                else if (this.detectAudioMetadataChange(sample)) {
                                    // flush stashed frames before notify new AudioSpecificConfig
                                    this.dispatchAudioMediaSegment();
                                    // notify new AAC AudioSpecificConfig
                                    this.dispatchAudioInitSegment(sample);
                                }
                            }
                        }
                        else if (tag === 0x80) {
                            if (elementary_PID === pmt.common_pids.av1) {
                                this.video_metadata_.av1c = data.subarray(offset + 2, offset + 2 + length_1);
                            }
                        }
                        offset += 2 + length_1;
                    }
                    // provide descriptor for PES private data via callback
                    var descriptors = data.subarray(i + 5, i + 5 + ES_info_length);
                    this.dispatchPESPrivateDataDescriptor(elementary_PID, stream_type, descriptors);
                }
            }
            else if (stream_type === _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kMetadata) {
                if (ES_info_length > 0) {
                    // parse descriptor for PES private data
                    for (var offset = i + 5; offset < i + 5 + ES_info_length;) {
                        var tag = data[offset + 0];
                        var length_2 = data[offset + 1];
                        if (tag === 0x26) {
                            var metadata_application_format = (data[offset + 2] << 8) | (data[offset + 3] << 0);
                            var metadata_application_format_identifier = null;
                            if (metadata_application_format === 0xFFFF) {
                                metadata_application_format_identifier = String.fromCharCode.apply(String, Array.from(data.subarray(offset + 4, offset + 4 + 4)));
                            }
                            var metadata_format = data[offset + 4 + (metadata_application_format === 0xFFFF ? 4 : 0)];
                            var metadata_format_identifier = null;
                            if (metadata_format === 0xFF) {
                                var pad = 4 + (metadata_application_format === 0xFFFF ? 4 : 0) + 1;
                                metadata_format_identifier = String.fromCharCode.apply(String, Array.from(data.subarray(offset + pad, offset + pad + 4)));
                            }
                            if (metadata_application_format_identifier === 'ID3 ' && metadata_format_identifier === 'ID3 ') {
                                pmt.timed_id3_pids[elementary_PID] = true;
                            }
                            else if (metadata_format_identifier === 'KLVA') {
                                pmt.synchronous_klv_pids[elementary_PID] = true;
                            }
                        }
                        offset += 2 + length_2;
                    }
                }
            }
            else if (stream_type === _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kSCTE35) {
                pmt.scte_35_pids[elementary_PID] = true;
            }
            else if (stream_type === _pat_pmt_pes__WEBPACK_IMPORTED_MODULE_4__["StreamType"].kPGS) {
                pmt.pgs_langs[elementary_PID] = 'und';
                if (ES_info_length > 0) {
                    // parse descriptor
                    for (var offset = i + 5; offset < i + 5 + ES_info_length;) {
                        var tag = data[offset + 0];
                        var length_3 = data[offset + 1];
                        if (tag === 0x0a) { // ISO_639_LANGUAGE_DESCRIPTOR
                            var lang = String.fromCharCode.apply(String, Array.from(data.slice(offset + 2, offset + 5)));
                            pmt.pgs_langs[elementary_PID] = lang;
                        }
                        offset += 2 + length_3;
                    }
                }
                pmt.pgs_pids[elementary_PID] = true;
            }
            i += 5 + ES_info_length;
        }
        if (program_number === this.current_program_) {
            if (this.pmt_ == undefined) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Parsed first PMT: ".concat(JSON.stringify(pmt)));
            }
            this.pmt_ = pmt;
            if (pmt.common_pids.h264 || pmt.common_pids.h265 || pmt.common_pids.av1) {
                this.has_video_ = true;
            }
            if (pmt.common_pids.adts_aac || pmt.common_pids.loas_aac || pmt.common_pids.ac3 || pmt.common_pids.opus || pmt.common_pids.mp3) {
                this.has_audio_ = true;
            }
        }
    };
    TSDemuxer.prototype.parseSCTE35 = function (data) {
        var scte35 = Object(_scte35__WEBPACK_IMPORTED_MODULE_9__["readSCTE35"])(data);
        if (scte35.pts != undefined) {
            var pts_ms = Math.floor(scte35.pts / this.timescale_);
            scte35.pts = pts_ms;
        }
        else {
            scte35.nearest_pts = this.getNearestTimestampMilliseconds();
        }
        if (this.onSCTE35Metadata) {
            this.onSCTE35Metadata(scte35);
        }
    };
    TSDemuxer.prototype.parseAV1Payload = function (data, pts, dts, file_position, random_access_indicator) {
        var av1_in_ts_parser = new _av1__WEBPACK_IMPORTED_MODULE_16__["default"](data);
        var payload = null;
        var units = [];
        var length = 0;
        var keyframe = false;
        var details = null;
        while ((payload = av1_in_ts_parser.readNextOBUPayload()) != null) {
            details = _av1_parser__WEBPACK_IMPORTED_MODULE_17__["default"].parseOBUs(payload, this.video_metadata_.details);
            if (details && details.keyframe === true) {
                if (!this.video_init_segment_dispatched_) {
                    var av1c = new Uint8Array((new ArrayBuffer(this.video_metadata_.av1c.byteLength + details.sequence_header_data.byteLength)));
                    av1c.set(this.video_metadata_.av1c, 0);
                    av1c.set(details.sequence_header_data, this.video_metadata_.av1c.byteLength);
                    details.av1c = av1c;
                    this.video_metadata_.details = details;
                    this.dispatchVideoInitSegment();
                }
                else if (this.detectVideoMetadataChange(null, details) === true) {
                    this.video_metadata_changed_ = true;
                    // flush stashed frames before changing codec metadata
                    this.dispatchVideoMediaSegment();
                    var av1c = new Uint8Array((new ArrayBuffer(this.video_metadata_.av1c.byteLength + details.sequence_header_data.byteLength)));
                    av1c.set(this.video_metadata_.av1c, 0);
                    av1c.set(details.sequence_header_data, this.video_metadata_.av1c.byteLength);
                    details.av1c = av1c;
                    // notify new codec metadata (maybe changed)
                    this.dispatchVideoInitSegment();
                }
            }
            this.video_metadata_.details = details;
            //if (this.video_init_segment_dispatched_) {
            keyframe || (keyframe = details.keyframe);
            units.push({ data: payload });
            length += payload.byteLength;
            //}
        }
        var pts_ms = Math.floor(pts / this.timescale_);
        var dts_ms = Math.floor(dts / this.timescale_);
        if (units.length) {
            var track = this.video_track_;
            var av1_sample = {
                units: units,
                length: length,
                isKeyframe: keyframe,
                dts: dts_ms,
                pts: pts_ms,
                cts: pts_ms - dts_ms,
                file_position: file_position
            };
            track.samples.push(av1_sample);
            track.length += length;
        }
    };
    TSDemuxer.prototype.parseH264Payload = function (data, pts, dts, file_position, random_access_indicator) {
        var annexb_parser = new _h264__WEBPACK_IMPORTED_MODULE_5__["H264AnnexBParser"](data);
        var nalu_payload = null;
        var units = [];
        var length = 0;
        var keyframe = false;
        while ((nalu_payload = annexb_parser.readNextNaluPayload()) != null) {
            var nalu_avc1 = new _h264__WEBPACK_IMPORTED_MODULE_5__["H264NaluAVC1"](nalu_payload);
            if (nalu_avc1.type === _h264__WEBPACK_IMPORTED_MODULE_5__["H264NaluType"].kSliceSPS) {
                // Notice: parseSPS requires Nalu without startcode or length-header
                var details = _sps_parser__WEBPACK_IMPORTED_MODULE_6__["default"].parseSPS(nalu_payload.data);
                if (!this.video_init_segment_dispatched_) {
                    this.video_metadata_.sps = nalu_avc1;
                    this.video_metadata_.details = details;
                }
                else if (this.detectVideoMetadataChange(nalu_avc1, details) === true) {
                    _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "H264: Critical h264 metadata has been changed, attempt to re-generate InitSegment");
                    this.video_metadata_changed_ = true;
                    this.video_metadata_ = { vps: undefined, sps: nalu_avc1, pps: undefined, av1c: undefined, details: details };
                }
            }
            else if (nalu_avc1.type === _h264__WEBPACK_IMPORTED_MODULE_5__["H264NaluType"].kSlicePPS) {
                if (!this.video_init_segment_dispatched_ || this.video_metadata_changed_) {
                    this.video_metadata_.pps = nalu_avc1;
                    if (this.video_metadata_.sps && this.video_metadata_.pps) {
                        if (this.video_metadata_changed_) {
                            // flush stashed frames before changing codec metadata
                            this.dispatchVideoMediaSegment();
                        }
                        // notify new codec metadata (maybe changed)
                        this.dispatchVideoInitSegment();
                    }
                }
            }
            else if (nalu_avc1.type === _h264__WEBPACK_IMPORTED_MODULE_5__["H264NaluType"].kSliceIDR) {
                keyframe = true;
            }
            else if (nalu_avc1.type === _h264__WEBPACK_IMPORTED_MODULE_5__["H264NaluType"].kSliceNonIDR && random_access_indicator === 1) {
                // For open-gop stream, use random_access_indicator to identify keyframe
                keyframe = true;
            }
            // Push samples to remuxer only if initialization metadata has been dispatched
            if (this.video_init_segment_dispatched_) {
                units.push(nalu_avc1);
                length += nalu_avc1.data.byteLength;
            }
        }
        var pts_ms = Math.floor(pts / this.timescale_);
        var dts_ms = Math.floor(dts / this.timescale_);
        if (units.length) {
            var track = this.video_track_;
            var avc_sample = {
                units: units,
                length: length,
                isKeyframe: keyframe,
                dts: dts_ms,
                pts: pts_ms,
                cts: pts_ms - dts_ms,
                file_position: file_position
            };
            track.samples.push(avc_sample);
            track.length += length;
        }
    };
    TSDemuxer.prototype.parseH265Payload = function (data, pts, dts, file_position, random_access_indicator) {
        var annexb_parser = new _h265__WEBPACK_IMPORTED_MODULE_10__["H265AnnexBParser"](data);
        var nalu_payload = null;
        var units = [];
        var length = 0;
        var keyframe = false;
        while ((nalu_payload = annexb_parser.readNextNaluPayload()) != null) {
            var nalu_hvc1 = new _h265__WEBPACK_IMPORTED_MODULE_10__["H265NaluHVC1"](nalu_payload);
            if (nalu_hvc1.type === _h265__WEBPACK_IMPORTED_MODULE_10__["H265NaluType"].kSliceVPS) {
                if (!this.video_init_segment_dispatched_) {
                    var details = _h265_parser__WEBPACK_IMPORTED_MODULE_11__["default"].parseVPS(nalu_payload.data);
                    this.video_metadata_.vps = nalu_hvc1;
                    this.video_metadata_.details = __assign(__assign({}, this.video_metadata_.details), details);
                }
            }
            else if (nalu_hvc1.type === _h265__WEBPACK_IMPORTED_MODULE_10__["H265NaluType"].kSliceSPS) {
                var details = _h265_parser__WEBPACK_IMPORTED_MODULE_11__["default"].parseSPS(nalu_payload.data);
                if (!this.video_init_segment_dispatched_) {
                    this.video_metadata_.sps = nalu_hvc1;
                    this.video_metadata_.details = __assign(__assign({}, this.video_metadata_.details), details);
                }
                else if (this.detectVideoMetadataChange(nalu_hvc1, details) === true) {
                    _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "H265: Critical h265 metadata has been changed, attempt to re-generate InitSegment");
                    this.video_metadata_changed_ = true;
                    this.video_metadata_ = { vps: undefined, sps: nalu_hvc1, pps: undefined, av1c: undefined, details: details };
                }
            }
            else if (nalu_hvc1.type === _h265__WEBPACK_IMPORTED_MODULE_10__["H265NaluType"].kSlicePPS) {
                if (!this.video_init_segment_dispatched_ || this.video_metadata_changed_) {
                    var details = _h265_parser__WEBPACK_IMPORTED_MODULE_11__["default"].parsePPS(nalu_payload.data);
                    this.video_metadata_.pps = nalu_hvc1;
                    this.video_metadata_.details = __assign(__assign({}, this.video_metadata_.details), details);
                    if (this.video_metadata_.vps && this.video_metadata_.sps && this.video_metadata_.pps) {
                        if (this.video_metadata_changed_) {
                            // flush stashed frames before changing codec metadata
                            this.dispatchVideoMediaSegment();
                        }
                        // notify new codec metadata (maybe changed)
                        this.dispatchVideoInitSegment();
                    }
                }
            }
            else if (nalu_hvc1.type === _h265__WEBPACK_IMPORTED_MODULE_10__["H265NaluType"].kSliceIDR_W_RADL || nalu_hvc1.type === _h265__WEBPACK_IMPORTED_MODULE_10__["H265NaluType"].kSliceIDR_N_LP || nalu_hvc1.type === _h265__WEBPACK_IMPORTED_MODULE_10__["H265NaluType"].kSliceCRA_NUT) {
                keyframe = true;
            }
            // Push samples to remuxer only if initialization metadata has been dispatched
            if (this.video_init_segment_dispatched_) {
                units.push(nalu_hvc1);
                length += nalu_hvc1.data.byteLength;
            }
        }
        var pts_ms = Math.floor(pts / this.timescale_);
        var dts_ms = Math.floor(dts / this.timescale_);
        if (units.length) {
            var track = this.video_track_;
            var hvc_sample = {
                units: units,
                length: length,
                isKeyframe: keyframe,
                dts: dts_ms,
                pts: pts_ms,
                cts: pts_ms - dts_ms,
                file_position: file_position
            };
            track.samples.push(hvc_sample);
            track.length += length;
        }
    };
    TSDemuxer.prototype.detectVideoMetadataChange = function (new_sps, new_details) {
        if (new_details.codec_mimetype !== this.video_metadata_.details.codec_mimetype) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Video: Codec mimeType changed from " +
                "".concat(this.video_metadata_.details.codec_mimetype, " to ").concat(new_details.codec_mimetype));
            return true;
        }
        if (new_details.codec_size.width !== this.video_metadata_.details.codec_size.width
            || new_details.codec_size.height !== this.video_metadata_.details.codec_size.height) {
            var old_size = this.video_metadata_.details.codec_size;
            var new_size = new_details.codec_size;
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Video: Coded Resolution changed from " +
                "".concat(old_size.width, "x").concat(old_size.height, " to ").concat(new_size.width, "x").concat(new_size.height));
            return true;
        }
        if (new_details.present_size.width !== this.video_metadata_.details.present_size.width) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Video: Present resolution width changed from " +
                "".concat(this.video_metadata_.details.present_size.width, " to ").concat(new_details.present_size.width));
            return true;
        }
        return false;
    };
    TSDemuxer.prototype.isInitSegmentDispatched = function () {
        if (this.has_video_ && this.has_audio_) { // both video & audio
            return this.video_init_segment_dispatched_ && this.audio_init_segment_dispatched_;
        }
        if (this.has_video_ && !this.has_audio_) { // video only
            return this.video_init_segment_dispatched_;
        }
        if (!this.has_video_ && this.has_audio_) { // audio only
            return this.audio_init_segment_dispatched_;
        }
        return false;
    };
    TSDemuxer.prototype.dispatchVideoInitSegment = function () {
        var details = this.video_metadata_.details;
        var meta = {};
        meta.type = 'video';
        meta.id = this.video_track_.id;
        meta.timescale = 1000;
        meta.duration = this.duration_;
        meta.codecWidth = details.codec_size.width;
        meta.codecHeight = details.codec_size.height;
        meta.presentWidth = details.present_size.width;
        meta.presentHeight = details.present_size.height;
        meta.profile = details.profile_string;
        meta.level = details.level_string;
        meta.bitDepth = details.bit_depth;
        meta.chromaFormat = details.chroma_format;
        meta.sarRatio = details.sar_ratio;
        meta.frameRate = details.frame_rate;
        var fps_den = meta.frameRate.fps_den;
        var fps_num = meta.frameRate.fps_num;
        meta.refSampleDuration = 1000 * (fps_den / fps_num);
        meta.codec = details.codec_mimetype;
        if (this.video_metadata_.av1c) {
            meta.av1c = this.video_metadata_.av1c;
            if (this.video_init_segment_dispatched_ == false) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Generated first AV1 for mimeType: ".concat(meta.codec));
            }
        }
        else if (this.video_metadata_.vps) {
            var vps_without_header = this.video_metadata_.vps.data.subarray(4);
            var sps_without_header = this.video_metadata_.sps.data.subarray(4);
            var pps_without_header = this.video_metadata_.pps.data.subarray(4);
            var hvcc = new _h265__WEBPACK_IMPORTED_MODULE_10__["HEVCDecoderConfigurationRecord"](vps_without_header, sps_without_header, pps_without_header, details);
            meta.hvcc = hvcc.getData();
            if (this.video_init_segment_dispatched_ == false) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Generated first HEVCDecoderConfigurationRecord for mimeType: ".concat(meta.codec));
            }
        }
        else {
            var sps_without_header = this.video_metadata_.sps.data.subarray(4);
            var pps_without_header = this.video_metadata_.pps.data.subarray(4);
            var avcc = new _h264__WEBPACK_IMPORTED_MODULE_5__["AVCDecoderConfigurationRecord"](sps_without_header, pps_without_header, details);
            meta.avcc = avcc.getData();
            if (this.video_init_segment_dispatched_ == false) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Generated first AVCDecoderConfigurationRecord for mimeType: ".concat(meta.codec));
            }
        }
        this.onTrackMetadata('video', meta);
        this.video_init_segment_dispatched_ = true;
        this.video_metadata_changed_ = false;
        // notify new MediaInfo
        var mi = this.media_info_;
        mi.hasVideo = true;
        mi.width = meta.codecWidth;
        mi.height = meta.codecHeight;
        mi.fps = meta.frameRate.fps;
        mi.profile = meta.profile;
        mi.level = meta.level;
        mi.refFrames = details.ref_frames;
        mi.chromaFormat = details.chroma_format_string;
        mi.sarNum = meta.sarRatio.width;
        mi.sarDen = meta.sarRatio.height;
        mi.videoCodec = meta.codec;
        if (mi.hasAudio && mi.audioCodec) {
            mi.mimeType = "video/mp2t; codecs=\"".concat(mi.videoCodec, ",").concat(mi.audioCodec, "\"");
        }
        else {
            mi.mimeType = "video/mp2t; codecs=\"".concat(mi.videoCodec, "\"");
        }
        if (mi.isComplete()) {
            this.onMediaInfo(mi);
        }
    };
    TSDemuxer.prototype.dispatchVideoMediaSegment = function () {
        if (this.isInitSegmentDispatched()) {
            if (this.video_track_.length) {
                this.onDataAvailable(null, this.video_track_);
            }
        }
    };
    TSDemuxer.prototype.dispatchAudioMediaSegment = function () {
        if (this.isInitSegmentDispatched()) {
            if (this.audio_track_.length) {
                this.onDataAvailable(this.audio_track_, null);
            }
        }
    };
    TSDemuxer.prototype.dispatchAudioVideoMediaSegment = function () {
        if (this.isInitSegmentDispatched()) {
            if (this.audio_track_.length || this.video_track_.length) {
                this.onDataAvailable(this.audio_track_, this.video_track_);
            }
        }
    };
    TSDemuxer.prototype.parseADTSAACPayload = function (data, pts) {
        if (this.has_video_ && !this.video_init_segment_dispatched_) {
            // If first video IDR frame hasn't been detected,
            // Wait for first IDR frame and video init segment being dispatched
            return;
        }
        if (this.aac_last_incomplete_data_) {
            var buf = new Uint8Array(data.byteLength + this.aac_last_incomplete_data_.byteLength);
            buf.set(this.aac_last_incomplete_data_, 0);
            buf.set(data, this.aac_last_incomplete_data_.byteLength);
            data = buf;
        }
        var ref_sample_duration;
        var base_pts_ms;
        if (pts != undefined) {
            base_pts_ms = pts / this.timescale_;
        }
        if (this.audio_metadata_.codec === 'aac') {
            if (pts == undefined && this.audio_last_sample_pts_ != undefined) {
                ref_sample_duration = 1024 / this.audio_metadata_.sampling_frequency * 1000;
                base_pts_ms = this.audio_last_sample_pts_ + ref_sample_duration;
            }
            else if (pts == undefined) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "AAC: Unknown pts");
                return;
            }
            if (this.aac_last_incomplete_data_ && this.audio_last_sample_pts_) {
                ref_sample_duration = 1024 / this.audio_metadata_.sampling_frequency * 1000;
                var new_pts_ms = this.audio_last_sample_pts_ + ref_sample_duration;
                if (Math.abs(new_pts_ms - base_pts_ms) > 1) {
                    _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "AAC: Detected pts overlapped, " +
                        "expected: ".concat(new_pts_ms, "ms, PES pts: ").concat(base_pts_ms, "ms"));
                    base_pts_ms = new_pts_ms;
                }
            }
        }
        var adts_parser = new _aac__WEBPACK_IMPORTED_MODULE_7__["AACADTSParser"](data);
        var aac_frame = null;
        var sample_pts_ms = base_pts_ms;
        var last_sample_pts_ms;
        while ((aac_frame = adts_parser.readNextAACFrame()) != null) {
            ref_sample_duration = 1024 / aac_frame.sampling_frequency * 1000;
            var audio_sample = {
                codec: 'aac',
                data: aac_frame
            };
            if (this.audio_init_segment_dispatched_ == false) {
                this.audio_metadata_ = {
                    codec: 'aac',
                    audio_object_type: aac_frame.audio_object_type,
                    sampling_freq_index: aac_frame.sampling_freq_index,
                    sampling_frequency: aac_frame.sampling_frequency,
                    channel_config: aac_frame.channel_config
                };
                this.dispatchAudioInitSegment(audio_sample);
            }
            else if (this.detectAudioMetadataChange(audio_sample)) {
                // flush stashed frames before notify new AudioSpecificConfig
                this.dispatchAudioMediaSegment();
                // notify new AAC AudioSpecificConfig
                this.dispatchAudioInitSegment(audio_sample);
            }
            last_sample_pts_ms = sample_pts_ms;
            var sample_pts_ms_int = Math.floor(sample_pts_ms);
            var aac_sample = {
                unit: aac_frame.data,
                length: aac_frame.data.byteLength,
                pts: sample_pts_ms_int,
                dts: sample_pts_ms_int
            };
            this.audio_track_.samples.push(aac_sample);
            this.audio_track_.length += aac_frame.data.byteLength;
            sample_pts_ms += ref_sample_duration;
        }
        if (adts_parser.hasIncompleteData()) {
            this.aac_last_incomplete_data_ = adts_parser.getIncompleteData();
        }
        if (last_sample_pts_ms) {
            this.audio_last_sample_pts_ = last_sample_pts_ms;
        }
    };
    TSDemuxer.prototype.parseLOASAACPayload = function (data, pts) {
        var _a;
        if (this.has_video_ && !this.video_init_segment_dispatched_) {
            // If first video IDR frame hasn't been detected,
            // Wait for first IDR frame and video init segment being dispatched
            return;
        }
        if (this.aac_last_incomplete_data_) {
            var buf = new Uint8Array(data.byteLength + this.aac_last_incomplete_data_.byteLength);
            buf.set(this.aac_last_incomplete_data_, 0);
            buf.set(data, this.aac_last_incomplete_data_.byteLength);
            data = buf;
        }
        var ref_sample_duration;
        var base_pts_ms;
        if (pts != undefined) {
            base_pts_ms = pts / this.timescale_;
        }
        if (this.audio_metadata_.codec === 'aac') {
            if (pts == undefined && this.audio_last_sample_pts_ != undefined) {
                ref_sample_duration = 1024 / this.audio_metadata_.sampling_frequency * 1000;
                base_pts_ms = this.audio_last_sample_pts_ + ref_sample_duration;
            }
            else if (pts == undefined) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "AAC: Unknown pts");
                return;
            }
            if (this.aac_last_incomplete_data_ && this.audio_last_sample_pts_) {
                ref_sample_duration = 1024 / this.audio_metadata_.sampling_frequency * 1000;
                var new_pts_ms = this.audio_last_sample_pts_ + ref_sample_duration;
                if (Math.abs(new_pts_ms - base_pts_ms) > 1) {
                    _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "AAC: Detected pts overlapped, " +
                        "expected: ".concat(new_pts_ms, "ms, PES pts: ").concat(base_pts_ms, "ms"));
                    base_pts_ms = new_pts_ms;
                }
            }
        }
        var loas_parser = new _aac__WEBPACK_IMPORTED_MODULE_7__["AACLOASParser"](data);
        var aac_frame = null;
        var sample_pts_ms = base_pts_ms;
        var last_sample_pts_ms;
        while ((aac_frame = loas_parser.readNextAACFrame((_a = this.loas_previous_frame) !== null && _a !== void 0 ? _a : undefined)) != null) {
            this.loas_previous_frame = aac_frame;
            ref_sample_duration = 1024 / aac_frame.sampling_frequency * 1000;
            var audio_sample = {
                codec: 'aac',
                data: aac_frame
            };
            if (this.audio_init_segment_dispatched_ == false) {
                this.audio_metadata_ = {
                    codec: 'aac',
                    audio_object_type: aac_frame.audio_object_type,
                    sampling_freq_index: aac_frame.sampling_freq_index,
                    sampling_frequency: aac_frame.sampling_frequency,
                    channel_config: aac_frame.channel_config
                };
                this.dispatchAudioInitSegment(audio_sample);
            }
            else if (this.detectAudioMetadataChange(audio_sample)) {
                // flush stashed frames before notify new AudioSpecificConfig
                this.dispatchAudioMediaSegment();
                // notify new AAC AudioSpecificConfig
                this.dispatchAudioInitSegment(audio_sample);
            }
            last_sample_pts_ms = sample_pts_ms;
            var sample_pts_ms_int = Math.floor(sample_pts_ms);
            var aac_sample = {
                unit: aac_frame.data,
                length: aac_frame.data.byteLength,
                pts: sample_pts_ms_int,
                dts: sample_pts_ms_int
            };
            this.audio_track_.samples.push(aac_sample);
            this.audio_track_.length += aac_frame.data.byteLength;
            sample_pts_ms += ref_sample_duration;
        }
        if (loas_parser.hasIncompleteData()) {
            this.aac_last_incomplete_data_ = loas_parser.getIncompleteData();
        }
        if (last_sample_pts_ms) {
            this.audio_last_sample_pts_ = last_sample_pts_ms;
        }
    };
    TSDemuxer.prototype.parseAC3Payload = function (data, pts) {
        if (this.has_video_ && !this.video_init_segment_dispatched_) {
            // If first video IDR frame hasn't been detected,
            // Wait for first IDR frame and video init segment being dispatched
            return;
        }
        var ref_sample_duration;
        var base_pts_ms;
        if (pts != undefined) {
            base_pts_ms = pts / this.timescale_;
        }
        if (this.audio_metadata_.codec === 'ac-3') {
            if (pts == undefined && this.audio_last_sample_pts_ != undefined) {
                ref_sample_duration = 1536 / this.audio_metadata_.sampling_frequency * 1000;
                base_pts_ms = this.audio_last_sample_pts_ + ref_sample_duration;
            }
            else if (pts == undefined) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "AC3: Unknown pts");
                return;
            }
        }
        var adts_parser = new _ac3__WEBPACK_IMPORTED_MODULE_14__["AC3Parser"](data);
        var ac3_frame = null;
        var sample_pts_ms = base_pts_ms;
        var last_sample_pts_ms;
        while ((ac3_frame = adts_parser.readNextAC3Frame()) != null) {
            ref_sample_duration = 1536 / ac3_frame.sampling_frequency * 1000;
            var audio_sample = {
                codec: 'ac-3',
                data: ac3_frame
            };
            if (this.audio_init_segment_dispatched_ == false) {
                this.audio_metadata_ = {
                    codec: 'ac-3',
                    sampling_frequency: ac3_frame.sampling_frequency,
                    bit_stream_identification: ac3_frame.bit_stream_identification,
                    bit_stream_mode: ac3_frame.bit_stream_mode,
                    low_frequency_effects_channel_on: ac3_frame.low_frequency_effects_channel_on,
                    channel_mode: ac3_frame.channel_mode,
                };
                this.dispatchAudioInitSegment(audio_sample);
            }
            else if (this.detectAudioMetadataChange(audio_sample)) {
                // flush stashed frames before notify new AudioSpecificConfig
                this.dispatchAudioMediaSegment();
                // notify new AAC AudioSpecificConfig
                this.dispatchAudioInitSegment(audio_sample);
            }
            last_sample_pts_ms = sample_pts_ms;
            var sample_pts_ms_int = Math.floor(sample_pts_ms);
            var ac3_sample = {
                unit: ac3_frame.data,
                length: ac3_frame.data.byteLength,
                pts: sample_pts_ms_int,
                dts: sample_pts_ms_int
            };
            this.audio_track_.samples.push(ac3_sample);
            this.audio_track_.length += ac3_frame.data.byteLength;
            sample_pts_ms += ref_sample_duration;
        }
        if (last_sample_pts_ms) {
            this.audio_last_sample_pts_ = last_sample_pts_ms;
        }
    };
    TSDemuxer.prototype.parseEAC3Payload = function (data, pts) {
        if (this.has_video_ && !this.video_init_segment_dispatched_) {
            // If first video IDR frame hasn't been detected,
            // Wait for first IDR frame and video init segment being dispatched
            return;
        }
        var ref_sample_duration;
        var base_pts_ms;
        if (pts != undefined) {
            base_pts_ms = pts / this.timescale_;
        }
        if (this.audio_metadata_.codec === 'ec-3') {
            if (pts == undefined && this.audio_last_sample_pts_ != undefined) {
                ref_sample_duration = (256 * this.audio_metadata_.num_blks) / this.audio_metadata_.sampling_frequency * 1000; // TODO: AEC3 BLK
                base_pts_ms = this.audio_last_sample_pts_ + ref_sample_duration;
            }
            else if (pts == undefined) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "EAC3: Unknown pts");
                return;
            }
        }
        var adts_parser = new _ac3__WEBPACK_IMPORTED_MODULE_14__["EAC3Parser"](data);
        var eac3_frame = null;
        var sample_pts_ms = base_pts_ms;
        var last_sample_pts_ms;
        while ((eac3_frame = adts_parser.readNextEAC3Frame()) != null) {
            ref_sample_duration = 1536 / eac3_frame.sampling_frequency * 1000; // TODO: EAC3 BLK
            var audio_sample = {
                codec: 'ec-3',
                data: eac3_frame
            };
            if (this.audio_init_segment_dispatched_ == false) {
                this.audio_metadata_ = {
                    codec: 'ec-3',
                    sampling_frequency: eac3_frame.sampling_frequency,
                    bit_stream_identification: eac3_frame.bit_stream_identification,
                    low_frequency_effects_channel_on: eac3_frame.low_frequency_effects_channel_on,
                    num_blks: eac3_frame.num_blks,
                    channel_mode: eac3_frame.channel_mode,
                };
                this.dispatchAudioInitSegment(audio_sample);
            }
            else if (this.detectAudioMetadataChange(audio_sample)) {
                // flush stashed frames before notify new AudioSpecificConfig
                this.dispatchAudioMediaSegment();
                // notify new AAC AudioSpecificConfig
                this.dispatchAudioInitSegment(audio_sample);
            }
            last_sample_pts_ms = sample_pts_ms;
            var sample_pts_ms_int = Math.floor(sample_pts_ms);
            var ac3_sample = {
                unit: eac3_frame.data,
                length: eac3_frame.data.byteLength,
                pts: sample_pts_ms_int,
                dts: sample_pts_ms_int
            };
            this.audio_track_.samples.push(ac3_sample);
            this.audio_track_.length += eac3_frame.data.byteLength;
            sample_pts_ms += ref_sample_duration;
        }
        if (last_sample_pts_ms) {
            this.audio_last_sample_pts_ = last_sample_pts_ms;
        }
    };
    TSDemuxer.prototype.parseOpusPayload = function (data, pts) {
        if (this.has_video_ && !this.video_init_segment_dispatched_) {
            // If first video IDR frame hasn't been detected,
            // Wait for first IDR frame and video init segment being dispatched
            return;
        }
        var ref_sample_duration;
        var base_pts_ms;
        if (pts != undefined) {
            base_pts_ms = pts / this.timescale_;
        }
        if (this.audio_metadata_.codec === 'opus') {
            if (pts == undefined && this.audio_last_sample_pts_ != undefined) {
                ref_sample_duration = 20;
                base_pts_ms = this.audio_last_sample_pts_ + ref_sample_duration;
            }
            else if (pts == undefined) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "Opus: Unknown pts");
                return;
            }
        }
        var sample_pts_ms = base_pts_ms;
        var last_sample_pts_ms;
        for (var offset = 0; offset < data.length;) {
            ref_sample_duration = 20;
            var opus_pending_trim_start = (data[offset + 1] & 0x10) !== 0;
            var trim_end = (data[offset + 1] & 0x08) !== 0;
            var index = offset + 2;
            var size = 0;
            while (data[index] === 0xFF) {
                size += 255;
                index += 1;
            }
            size += data[index];
            index += 1;
            index += opus_pending_trim_start ? 2 : 0;
            index += trim_end ? 2 : 0;
            last_sample_pts_ms = sample_pts_ms;
            var sample_pts_ms_int = Math.floor(sample_pts_ms);
            var sample = data.slice(index, index + size);
            var opus_sample = {
                unit: sample,
                length: sample.byteLength,
                pts: sample_pts_ms_int,
                dts: sample_pts_ms_int
            };
            this.audio_track_.samples.push(opus_sample);
            this.audio_track_.length += sample.byteLength;
            sample_pts_ms += ref_sample_duration;
            offset = index + size;
        }
        if (last_sample_pts_ms) {
            this.audio_last_sample_pts_ = last_sample_pts_ms;
        }
    };
    TSDemuxer.prototype.parseMP3Payload = function (data, pts) {
        if (this.has_video_ && !this.video_init_segment_dispatched_) {
            // If first video IDR frame hasn't been detected,
            // Wait for first IDR frame and video init segment being dispatched
            return;
        }
        var _mpegAudioV10SampleRateTable = [44100, 48000, 32000, 0];
        var _mpegAudioV20SampleRateTable = [22050, 24000, 16000, 0];
        var _mpegAudioV25SampleRateTable = [11025, 12000, 8000, 0];
        var _mpegAudioL1BitRateTable = [0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, -1];
        var _mpegAudioL2BitRateTable = [0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, -1];
        var _mpegAudioL3BitRateTable = [0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, -1];
        var ver = (data[1] >>> 3) & 0x03;
        var layer = (data[1] & 0x06) >> 1;
        var bitrate_index = (data[2] & 0xF0) >>> 4;
        var sampling_freq_index = (data[2] & 0x0C) >>> 2;
        var channel_mode = (data[3] >>> 6) & 0x03;
        var channel_count = channel_mode !== 3 ? 2 : 1;
        var sample_rate = 0;
        var bit_rate = 0;
        var object_type = 34; // Layer-3, listed in MPEG-4 Audio Object Types
        var codec = 'mp3';
        switch (ver) {
            case 0: // MPEG 2.5
                sample_rate = _mpegAudioV25SampleRateTable[sampling_freq_index];
                break;
            case 2: // MPEG 2
                sample_rate = _mpegAudioV20SampleRateTable[sampling_freq_index];
                break;
            case 3: // MPEG 1
                sample_rate = _mpegAudioV10SampleRateTable[sampling_freq_index];
                break;
        }
        switch (layer) {
            case 1: // Layer 3
                object_type = 34;
                if (bitrate_index < _mpegAudioL3BitRateTable.length) {
                    bit_rate = _mpegAudioL3BitRateTable[bitrate_index];
                }
                break;
            case 2: // Layer 2
                object_type = 33;
                if (bitrate_index < _mpegAudioL2BitRateTable.length) {
                    bit_rate = _mpegAudioL2BitRateTable[bitrate_index];
                }
                break;
            case 3: // Layer 1
                object_type = 32;
                if (bitrate_index < _mpegAudioL1BitRateTable.length) {
                    bit_rate = _mpegAudioL1BitRateTable[bitrate_index];
                }
                break;
        }
        var sample = new _mp3__WEBPACK_IMPORTED_MODULE_13__["MP3Data"]();
        sample.object_type = object_type;
        sample.sample_rate = sample_rate;
        sample.channel_count = channel_count;
        sample.data = data;
        var audio_sample = {
            codec: 'mp3',
            data: sample
        };
        if (this.audio_init_segment_dispatched_ == false) {
            this.audio_metadata_ = {
                codec: 'mp3',
                object_type: object_type,
                sample_rate: sample_rate,
                channel_count: channel_count
            };
            this.dispatchAudioInitSegment(audio_sample);
        }
        else if (this.detectAudioMetadataChange(audio_sample)) {
            // flush stashed frames before notify new AudioSpecificConfig
            this.dispatchAudioMediaSegment();
            // notify new AAC AudioSpecificConfig
            this.dispatchAudioInitSegment(audio_sample);
        }
        var mp3_sample = {
            unit: data,
            length: data.byteLength,
            pts: pts / this.timescale_,
            dts: pts / this.timescale_
        };
        this.audio_track_.samples.push(mp3_sample);
        this.audio_track_.length += data.byteLength;
    };
    TSDemuxer.prototype.detectAudioMetadataChange = function (sample) {
        if (sample.codec !== this.audio_metadata_.codec) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Audio: Audio Codecs changed from " +
                "".concat(this.audio_metadata_.codec, " to ").concat(sample.codec));
            return true;
        }
        if (sample.codec === 'aac' && this.audio_metadata_.codec === 'aac') {
            var frame = sample.data;
            if (frame.audio_object_type !== this.audio_metadata_.audio_object_type) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "AAC: AudioObjectType changed from " +
                    "".concat(this.audio_metadata_.audio_object_type, " to ").concat(frame.audio_object_type));
                return true;
            }
            if (frame.sampling_freq_index !== this.audio_metadata_.sampling_freq_index) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "AAC: SamplingFrequencyIndex changed from " +
                    "".concat(this.audio_metadata_.sampling_freq_index, " to ").concat(frame.sampling_freq_index));
                return true;
            }
            if (frame.channel_config !== this.audio_metadata_.channel_config) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "AAC: Channel configuration changed from " +
                    "".concat(this.audio_metadata_.channel_config, " to ").concat(frame.channel_config));
                return true;
            }
        }
        else if (sample.codec === 'ac-3' && this.audio_metadata_.codec === 'ac-3') {
            var frame = sample.data;
            if (frame.sampling_frequency !== this.audio_metadata_.sampling_frequency) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "AC3: Sampling Frequency changed from " +
                    "".concat(this.audio_metadata_.sampling_frequency, " to ").concat(frame.sampling_frequency));
                return true;
            }
            if (frame.bit_stream_identification !== this.audio_metadata_.bit_stream_identification) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "AC3: Bit Stream Identification changed from " +
                    "".concat(this.audio_metadata_.bit_stream_identification, " to ").concat(frame.bit_stream_identification));
                return true;
            }
            if (frame.bit_stream_mode !== this.audio_metadata_.bit_stream_mode) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "AC3: BitStream Mode changed from " +
                    "".concat(this.audio_metadata_.bit_stream_mode, " to ").concat(frame.bit_stream_mode));
                return true;
            }
            if (frame.channel_mode !== this.audio_metadata_.channel_mode) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "AC3: Channel Mode changed from " +
                    "".concat(this.audio_metadata_.channel_mode, " to ").concat(frame.channel_mode));
                return true;
            }
            if (frame.low_frequency_effects_channel_on !== this.audio_metadata_.low_frequency_effects_channel_on) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "AC3: Low Frequency Effects Channel On changed from " +
                    "".concat(this.audio_metadata_.low_frequency_effects_channel_on, " to ").concat(frame.low_frequency_effects_channel_on));
                return true;
            }
        }
        else if (sample.codec === 'opus' && this.audio_metadata_.codec === 'opus') {
            var data = sample.meta;
            if (data.sample_rate !== this.audio_metadata_.sample_rate) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Opus: SamplingFrequencyIndex changed from " +
                    "".concat(this.audio_metadata_.sample_rate, " to ").concat(data.sample_rate));
                return true;
            }
            if (data.channel_count !== this.audio_metadata_.channel_count) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Opus: Channel count changed from " +
                    "".concat(this.audio_metadata_.channel_count, " to ").concat(data.channel_count));
                return true;
            }
        }
        else if (sample.codec === 'mp3' && this.audio_metadata_.codec === 'mp3') {
            var data = sample.data;
            if (data.object_type !== this.audio_metadata_.object_type) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "MP3: AudioObjectType changed from " +
                    "".concat(this.audio_metadata_.object_type, " to ").concat(data.object_type));
                return true;
            }
            if (data.sample_rate !== this.audio_metadata_.sample_rate) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "MP3: SamplingFrequencyIndex changed from " +
                    "".concat(this.audio_metadata_.sample_rate, " to ").concat(data.sample_rate));
                return true;
            }
            if (data.channel_count !== this.audio_metadata_.channel_count) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "MP3: Channel count changed from " +
                    "".concat(this.audio_metadata_.channel_count, " to ").concat(data.channel_count));
                return true;
            }
        }
        return false;
    };
    TSDemuxer.prototype.dispatchAudioInitSegment = function (sample) {
        var meta = {};
        meta.type = 'audio';
        meta.id = this.audio_track_.id;
        meta.timescale = 1000;
        meta.duration = this.duration_;
        if (this.audio_metadata_.codec === 'aac') {
            var aac_frame = sample.codec === 'aac' ? sample.data : null;
            var audio_specific_config = new _aac__WEBPACK_IMPORTED_MODULE_7__["AudioSpecificConfig"](aac_frame);
            meta.audioSampleRate = audio_specific_config.sampling_rate;
            meta.channelCount = audio_specific_config.channel_count;
            meta.codec = audio_specific_config.codec_mimetype;
            meta.originalCodec = audio_specific_config.original_codec_mimetype;
            meta.config = audio_specific_config.config;
            meta.refSampleDuration = 1024 / meta.audioSampleRate * meta.timescale;
        }
        else if (this.audio_metadata_.codec === 'ac-3') {
            var ac3_frame = sample.codec === 'ac-3' ? sample.data : null;
            var ac3_config = new _ac3__WEBPACK_IMPORTED_MODULE_14__["AC3Config"](ac3_frame);
            meta.audioSampleRate = ac3_config.sampling_rate;
            meta.channelCount = ac3_config.channel_count;
            meta.codec = ac3_config.codec_mimetype;
            meta.originalCodec = ac3_config.original_codec_mimetype;
            meta.config = ac3_config.config;
            meta.refSampleDuration = 1536 / meta.audioSampleRate * meta.timescale;
        }
        else if (this.audio_metadata_.codec === 'ec-3') {
            var ec3_frame = sample.codec === 'ec-3' ? sample.data : null;
            var ec3_config = new _ac3__WEBPACK_IMPORTED_MODULE_14__["EAC3Config"](ec3_frame);
            meta.audioSampleRate = ec3_config.sampling_rate;
            meta.channelCount = ec3_config.channel_count;
            meta.codec = ec3_config.codec_mimetype;
            meta.originalCodec = ec3_config.original_codec_mimetype;
            meta.config = ec3_config.config;
            meta.refSampleDuration = (256 * ec3_config.num_blks) / meta.audioSampleRate * meta.timescale; // TODO: blk size
        }
        else if (this.audio_metadata_.codec === 'opus') {
            meta.audioSampleRate = this.audio_metadata_.sample_rate;
            meta.channelCount = this.audio_metadata_.channel_count;
            meta.channelConfigCode = this.audio_metadata_.channel_config_code;
            meta.codec = 'opus';
            meta.originalCodec = 'opus';
            meta.config = undefined;
            meta.refSampleDuration = 20;
        }
        else if (this.audio_metadata_.codec === 'mp3') {
            meta.audioSampleRate = this.audio_metadata_.sample_rate;
            meta.channelCount = this.audio_metadata_.channel_count;
            meta.codec = 'mp3';
            meta.originalCodec = 'mp3';
            meta.config = undefined;
        }
        if (this.audio_init_segment_dispatched_ == false) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Generated first AudioSpecificConfig for mimeType: ".concat(meta.codec));
        }
        this.onTrackMetadata('audio', meta);
        this.audio_init_segment_dispatched_ = true;
        this.video_metadata_changed_ = false;
        // notify new MediaInfo
        var mi = this.media_info_;
        mi.hasAudio = true;
        mi.audioCodec = meta.originalCodec;
        mi.audioSampleRate = meta.audioSampleRate;
        mi.audioChannelCount = meta.channelCount;
        if (mi.hasVideo && mi.videoCodec) {
            mi.mimeType = "video/mp2t; codecs=\"".concat(mi.videoCodec, ",").concat(mi.audioCodec, "\"");
        }
        else {
            mi.mimeType = "video/mp2t; codecs=\"".concat(mi.audioCodec, "\"");
        }
        if (mi.isComplete()) {
            this.onMediaInfo(mi);
        }
    };
    TSDemuxer.prototype.dispatchPESPrivateDataDescriptor = function (pid, stream_type, descriptor) {
        var desc = new _pes_private_data__WEBPACK_IMPORTED_MODULE_8__["PESPrivateDataDescriptor"]();
        desc.pid = pid;
        desc.stream_type = stream_type;
        desc.descriptor = descriptor;
        if (this.onPESPrivateDataDescriptor) {
            this.onPESPrivateDataDescriptor(desc);
        }
    };
    TSDemuxer.prototype.parsePESPrivateDataPayload = function (data, pts, dts, pid, stream_id) {
        var private_data = new _pes_private_data__WEBPACK_IMPORTED_MODULE_8__["PESPrivateData"]();
        private_data.pid = pid;
        private_data.stream_id = stream_id;
        private_data.len = data.byteLength;
        private_data.data = data;
        if (pts != undefined) {
            var pts_ms = Math.floor(pts / this.timescale_);
            private_data.pts = pts_ms;
        }
        else {
            private_data.nearest_pts = this.getNearestTimestampMilliseconds();
        }
        if (dts != undefined) {
            var dts_ms = Math.floor(dts / this.timescale_);
            private_data.dts = dts_ms;
        }
        if (this.onPESPrivateData) {
            this.onPESPrivateData(private_data);
        }
    };
    TSDemuxer.prototype.parseTimedID3MetadataPayload = function (data, pts, dts, pid, stream_id) {
        var timed_id3_metadata = new _pes_private_data__WEBPACK_IMPORTED_MODULE_8__["PESPrivateData"]();
        timed_id3_metadata.pid = pid;
        timed_id3_metadata.stream_id = stream_id;
        timed_id3_metadata.len = data.byteLength;
        timed_id3_metadata.data = data;
        if (pts != undefined) {
            var pts_ms = Math.floor(pts / this.timescale_);
            timed_id3_metadata.pts = pts_ms;
        }
        if (dts != undefined) {
            var dts_ms = Math.floor(dts / this.timescale_);
            timed_id3_metadata.dts = dts_ms;
        }
        if (this.onTimedID3Metadata) {
            this.onTimedID3Metadata(timed_id3_metadata);
        }
    };
    TSDemuxer.prototype.parsePGSPayload = function (data, pts, dts, pid, stream_id, lang) {
        var pgs_data = new _pgs_data__WEBPACK_IMPORTED_MODULE_18__["PGSData"]();
        pgs_data.pid = pid;
        pgs_data.lang = lang;
        pgs_data.stream_id = stream_id;
        pgs_data.len = data.byteLength;
        pgs_data.data = data;
        if (pts != undefined) {
            var pts_ms = Math.floor(pts / this.timescale_);
            pgs_data.pts = pts_ms;
        }
        if (dts != undefined) {
            var dts_ms = Math.floor(dts / this.timescale_);
            pgs_data.dts = dts_ms;
        }
        if (this.onPGSSubtitleData) {
            this.onPGSSubtitleData(pgs_data);
        }
    };
    TSDemuxer.prototype.parseSynchronousKLVMetadataPayload = function (data, pts, dts, pid, stream_id) {
        var synchronous_klv_metadata = new _klv__WEBPACK_IMPORTED_MODULE_15__["KLVData"]();
        synchronous_klv_metadata.pid = pid;
        synchronous_klv_metadata.stream_id = stream_id;
        synchronous_klv_metadata.len = data.byteLength;
        synchronous_klv_metadata.data = data;
        if (pts != undefined) {
            var pts_ms = Math.floor(pts / this.timescale_);
            synchronous_klv_metadata.pts = pts_ms;
        }
        if (dts != undefined) {
            var dts_ms = Math.floor(dts / this.timescale_);
            synchronous_klv_metadata.dts = dts_ms;
        }
        synchronous_klv_metadata.access_units = Object(_klv__WEBPACK_IMPORTED_MODULE_15__["klv_parse"])(data);
        if (this.onSynchronousKLVMetadata) {
            this.onSynchronousKLVMetadata(synchronous_klv_metadata);
        }
    };
    TSDemuxer.prototype.parseAsynchronousKLVMetadataPayload = function (data, pid, stream_id) {
        var asynchronous_klv_metadata = new _pes_private_data__WEBPACK_IMPORTED_MODULE_8__["PESPrivateData"]();
        asynchronous_klv_metadata.pid = pid;
        asynchronous_klv_metadata.stream_id = stream_id;
        asynchronous_klv_metadata.len = data.byteLength;
        asynchronous_klv_metadata.data = data;
        if (this.onAsynchronousKLVMetadata) {
            this.onAsynchronousKLVMetadata(asynchronous_klv_metadata);
        }
    };
    TSDemuxer.prototype.parseSMPTE2038MetadataPayload = function (data, pts, dts, pid, stream_id) {
        var smpte2038_data = new _smpte2038__WEBPACK_IMPORTED_MODULE_12__["SMPTE2038Data"]();
        smpte2038_data.pid = pid;
        smpte2038_data.stream_id = stream_id;
        smpte2038_data.len = data.byteLength;
        smpte2038_data.data = data;
        if (pts != undefined) {
            var pts_ms = Math.floor(pts / this.timescale_);
            smpte2038_data.pts = pts_ms;
        }
        smpte2038_data.nearest_pts = this.getNearestTimestampMilliseconds();
        if (dts != undefined) {
            var dts_ms = Math.floor(dts / this.timescale_);
            smpte2038_data.dts = dts_ms;
        }
        smpte2038_data.ancillaries = Object(_smpte2038__WEBPACK_IMPORTED_MODULE_12__["smpte2038parse"])(data);
        if (this.onSMPTE2038Metadata) {
            this.onSMPTE2038Metadata(smpte2038_data);
        }
    };
    TSDemuxer.prototype.getNearestTimestampMilliseconds = function () {
        // Prefer using last audio sample pts if audio track exists
        if (this.audio_last_sample_pts_ != undefined) {
            return Math.floor(this.audio_last_sample_pts_);
        }
        else if (this.last_pcr_ != undefined) {
            // Fallback to PCR time if audio track doesn't exist
            var pcr_time_ms = Math.floor(this.last_pcr_ / 300 / this.timescale_);
            return pcr_time_ms;
        }
        return undefined;
    };
    TSDemuxer.prototype.getPcrBase = function (data) {
        var pcr_base = data[6] * 33554432 // 1 << 25
            + data[7] * 131072 // 1 << 17
            + data[8] * 512 // 1 << 9
            + data[9] * 2 // 1 << 1
            + (data[10] & 0x80) / 128 // 1 >> 7
            + this.timestamp_offset_;
        if (pcr_base + 0x100000000 < this.last_pcr_base_) {
            pcr_base += 0x200000000; // pcr_base wraparound
            this.timestamp_offset_ += 0x200000000;
        }
        this.last_pcr_base_ = pcr_base;
        return pcr_base;
    };
    TSDemuxer.prototype.getTimestamp = function (data, pos) {
        var timestamp = (data[pos] & 0x0E) * 536870912 // 1 << 29
            + (data[pos + 1] & 0xFF) * 4194304 // 1 << 22
            + (data[pos + 2] & 0xFE) * 16384 // 1 << 14
            + (data[pos + 3] & 0xFF) * 128 // 1 << 7
            + (data[pos + 4] & 0xFE) / 2
            + this.timestamp_offset_;
        if (timestamp + 0x100000000 < this.last_pcr_base_) {
            timestamp += 0x200000000; // pts/dts wraparound
        }
        return timestamp;
    };
    return TSDemuxer;
}(_base_demuxer__WEBPACK_IMPORTED_MODULE_3__["default"]));
/* harmony default export */ __webpack_exports__["default"] = (TSDemuxer);


/***/ }),

/***/ "./src/index.js":
/*!**********************!*\
  !*** ./src/index.js ***!
  \**********************/
/*! no static exports found */
/***/ (function(module, exports, __webpack_require__) {

// entry/index file
// make it compatible with browserify's umd wrapper
module.exports = __webpack_require__(/*! ./mpegts.js */ "./src/mpegts.js").default;


/***/ }),

/***/ "./src/io/amf-encoder.js":
/*!*******************************!*\
  !*** ./src/io/amf-encoder.js ***!
  \*******************************/
/*! exports provided: encodeCommandMessage, createConnectCommand, createReleaseStreamCommand, createFCPublishCommand, createCreateStreamCommand, createPlayCommand, createDeleteStreamCommand, default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* WEBPACK VAR INJECTION */(function(Buffer) {/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "encodeCommandMessage", function() { return encodeCommandMessage; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "createConnectCommand", function() { return createConnectCommand; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "createReleaseStreamCommand", function() { return createReleaseStreamCommand; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "createFCPublishCommand", function() { return createFCPublishCommand; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "createCreateStreamCommand", function() { return createCreateStreamCommand; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "createPlayCommand", function() { return createPlayCommand; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "createDeleteStreamCommand", function() { return createDeleteStreamCommand; });
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/**
 * AMF0 Encoder for RTMP Commands
 * Encodes JavaScript values to AMF0 binary format
 */
// AMF0 Type Markers
var AMF0Type = {
    NUMBER: 0,
    BOOLEAN: 1,
    STRING: 2,
    OBJECT: 3,
    NULL: 5,
    UNDEFINED: 6,
    OBJECT_END: 9
};
/**
 * Encode a number (8-byte IEEE-754 double)
 */
function encodeNumber(value) {
    var buffer = Buffer.allocUnsafe(9);
    buffer[0] = AMF0Type.NUMBER;
    buffer.writeDoubleBE(value, 1);
    return buffer;
}
/**
 * Encode a boolean (1 byte)
 */
function encodeBoolean(value) {
    var buffer = Buffer.allocUnsafe(2);
    buffer[0] = AMF0Type.BOOLEAN;
    buffer[1] = value ? 1 : 0;
    return buffer;
}
/**
 * Encode a string (2-byte length + UTF-8 string)
 */
function encodeString(value) {
    var strBuffer = Buffer.from(value, 'utf8');
    var length = strBuffer.length;
    var buffer = Buffer.allocUnsafe(3 + length);
    buffer[0] = AMF0Type.STRING;
    buffer.writeUInt16BE(length, 1);
    strBuffer.copy(buffer, 3);
    return buffer;
}
/**
 * Encode a short string without type marker (used in object properties)
 */
function encodeShortString(value) {
    var strBuffer = Buffer.from(value, 'utf8');
    var length = strBuffer.length;
    var buffer = Buffer.allocUnsafe(2 + length);
    buffer.writeUInt16BE(length, 0);
    strBuffer.copy(buffer, 2);
    return buffer;
}
/**
 * Encode null
 */
function encodeNull() {
    return Buffer.from([AMF0Type.NULL]);
}
/**
 * Encode undefined
 */
function encodeUndefined() {
    return Buffer.from([AMF0Type.UNDEFINED]);
}
/**
 * Encode an object (key-value pairs terminated by object-end marker)
 */
function encodeObject(obj) {
    var parts = [Buffer.from([AMF0Type.OBJECT])];
    for (var _i = 0, _a = Object.entries(obj); _i < _a.length; _i++) {
        var _b = _a[_i], key = _b[0], value = _b[1];
        // Encode property name (short string without type marker)
        parts.push(encodeShortString(key));
        // Encode property value
        parts.push(encodeValue(value));
    }
    // Object end marker (0x00 0x00 0x09)
    parts.push(Buffer.from([0x00, 0x00, AMF0Type.OBJECT_END]));
    return Buffer.concat(parts);
}
/**
 * Encode any AMF0 value based on its JavaScript type
 */
function encodeValue(value) {
    if (value === null) {
        return encodeNull();
    }
    else if (value === undefined) {
        return encodeUndefined();
    }
    else if (typeof value === 'number') {
        return encodeNumber(value);
    }
    else if (typeof value === 'boolean') {
        return encodeBoolean(value);
    }
    else if (typeof value === 'string') {
        return encodeString(value);
    }
    else if (typeof value === 'object') {
        return encodeObject(value);
    }
    else {
        throw new Error("Unsupported AMF0 type: ".concat(typeof value));
    }
}
/**
 * Encode RTMP command message
 * Format: command_name (string) + transaction_id (number) + command_object + ...args
 */
function encodeCommandMessage(commandName, transactionId, commandObject) {
    var args = [];
    for (var _i = 3; _i < arguments.length; _i++) {
        args[_i - 3] = arguments[_i];
    }
    var parts = [];
    // Command name (string)
    parts.push(encodeString(commandName));
    // Transaction ID (number)
    parts.push(encodeNumber(transactionId));
    // Command object (can be null or object)
    if (commandObject === null) {
        parts.push(encodeNull());
    }
    else {
        parts.push(encodeValue(commandObject));
    }
    // Additional arguments
    for (var _a = 0, args_1 = args; _a < args_1.length; _a++) {
        var arg = args_1[_a];
        parts.push(encodeValue(arg));
    }
    return Buffer.concat(parts);
}
/**
 * Create connect() command
 */
function createConnectCommand(app, tcUrl) {
    var commandObject = {
        app: app,
        flashVer: 'FMLE/3.0 (compatible; mpegts.js)',
        tcUrl: tcUrl,
        fpad: false,
        capabilities: 15,
        audioCodecs: 3191,
        videoCodecs: 252,
        videoFunction: 1,
        objectEncoding: 0
    };
    return encodeCommandMessage('connect', 1, commandObject);
}
/**
 * Create releaseStream() command (optional, for publishing)
 */
function createReleaseStreamCommand(streamName) {
    return encodeCommandMessage('releaseStream', 2, null, streamName);
}
/**
 * Create FCPublish() command (optional, for publishing)
 */
function createFCPublishCommand(streamName) {
    return encodeCommandMessage('FCPublish', 3, null, streamName);
}
/**
 * Create createStream() command
 */
function createCreateStreamCommand() {
    return encodeCommandMessage('createStream', 2, null);
}
/**
 * Create play() command
 * @param streamName - Name of the stream to play
 * @param start - Start time in seconds (default -2 for live)
 * @param duration - Duration in seconds (default -1 for all)
 * @param reset - Whether to flush previous playlist (default true)
 */
function createPlayCommand(streamName, start, duration, reset) {
    if (start === void 0) { start = -2; }
    if (duration === void 0) { duration = -1; }
    if (reset === void 0) { reset = true; }
    return encodeCommandMessage('play', 0, null, streamName, start, duration, reset);
}
/**
 * Create deleteStream() command
 */
function createDeleteStreamCommand(streamId) {
    return encodeCommandMessage('deleteStream', 0, null, streamId);
}
/* harmony default export */ __webpack_exports__["default"] = ({
    encodeCommandMessage: encodeCommandMessage,
    createConnectCommand: createConnectCommand,
    createReleaseStreamCommand: createReleaseStreamCommand,
    createFCPublishCommand: createFCPublishCommand,
    createCreateStreamCommand: createCreateStreamCommand,
    createPlayCommand: createPlayCommand,
    createDeleteStreamCommand: createDeleteStreamCommand
});

/* WEBPACK VAR INJECTION */}.call(this, __webpack_require__(/*! ./../../node_modules/buffer/index.js */ "./node_modules/buffer/index.js").Buffer))

/***/ }),

/***/ "./src/io/fetch-stream-loader.js":
/*!***************************************!*\
  !*** ./src/io/fetch-stream-loader.js ***!
  \***************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/* harmony import */ var _utils_browser_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ../utils/browser.js */ "./src/utils/browser.js");
/* harmony import */ var _loader_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ./loader.js */ "./src/io/loader.js");
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ../utils/exception.js */ "./src/utils/exception.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var __extends = (undefined && undefined.__extends) || (function () {
    var extendStatics = function (d, b) {
        extendStatics = Object.setPrototypeOf ||
            ({ __proto__: [] } instanceof Array && function (d, b) { d.__proto__ = b; }) ||
            function (d, b) { for (var p in b) if (Object.prototype.hasOwnProperty.call(b, p)) d[p] = b[p]; };
        return extendStatics(d, b);
    };
    return function (d, b) {
        if (typeof b !== "function" && b !== null)
            throw new TypeError("Class extends value " + String(b) + " is not a constructor or null");
        extendStatics(d, b);
        function __() { this.constructor = d; }
        d.prototype = b === null ? Object.create(b) : (__.prototype = b.prototype, new __());
    };
})();




/* fetch + stream IO loader. Currently working on chrome 43+.
 * fetch provides a better alternative http API to XMLHttpRequest
 *
 * fetch spec   https://fetch.spec.whatwg.org/
 * stream spec  https://streams.spec.whatwg.org/
 */
var FetchStreamLoader = /** @class */ (function (_super) {
    __extends(FetchStreamLoader, _super);
    function FetchStreamLoader(seekHandler, config) {
        var _this = _super.call(this, 'fetch-stream-loader') || this;
        _this.TAG = 'FetchStreamLoader';
        _this._seekHandler = seekHandler;
        _this._config = config;
        _this._needStash = true;
        _this._requestAbort = false;
        _this._abortController = null;
        _this._contentLength = null;
        _this._receivedLength = 0;
        return _this;
    }
    FetchStreamLoader.isSupported = function () {
        try {
            // fetch + stream is broken on Microsoft Edge. Disable before build 15048.
            // see https://developer.microsoft.com/en-us/microsoft-edge/platform/issues/8196907/
            // Fixed in Jan 10, 2017. Build 15048+ removed from blacklist.
            var isWorkWellEdge = _utils_browser_js__WEBPACK_IMPORTED_MODULE_1__["default"].msedge && _utils_browser_js__WEBPACK_IMPORTED_MODULE_1__["default"].version.minor >= 15048;
            var browserNotBlacklisted = _utils_browser_js__WEBPACK_IMPORTED_MODULE_1__["default"].msedge ? isWorkWellEdge : true;
            return (self.fetch && self.ReadableStream && browserNotBlacklisted);
        }
        catch (e) {
            return false;
        }
    };
    FetchStreamLoader.prototype.destroy = function () {
        if (this.isWorking()) {
            this.abort();
        }
        _super.prototype.destroy.call(this);
    };
    FetchStreamLoader.prototype.open = function (dataSource, range) {
        var _this = this;
        this._dataSource = dataSource;
        this._range = range;
        var sourceURL = dataSource.url;
        if (this._config.reuseRedirectedURL && dataSource.redirectedURL != undefined) {
            sourceURL = dataSource.redirectedURL;
        }
        var seekConfig = this._seekHandler.getConfig(sourceURL, range);
        var headers = new self.Headers();
        if (typeof seekConfig.headers === 'object') {
            var configHeaders = seekConfig.headers;
            for (var key in configHeaders) {
                if (configHeaders.hasOwnProperty(key)) {
                    headers.append(key, configHeaders[key]);
                }
            }
        }
        var params = {
            method: 'GET',
            headers: headers,
            mode: 'cors',
            cache: 'default',
            // The default policy of Fetch API in the whatwg standard
            // Safari incorrectly indicates 'no-referrer' as default policy, fuck it
            referrerPolicy: 'no-referrer-when-downgrade'
        };
        // add additional headers
        if (typeof this._config.headers === 'object') {
            for (var key in this._config.headers) {
                headers.append(key, this._config.headers[key]);
            }
        }
        // cors is enabled by default
        if (dataSource.cors === false) {
            // no-cors means 'disregard cors policy', which can only be used in ServiceWorker
            params.mode = 'same-origin';
        }
        // withCredentials is disabled by default
        if (dataSource.withCredentials) {
            params.credentials = 'include';
        }
        // referrerPolicy from config
        if (dataSource.referrerPolicy) {
            params.referrerPolicy = dataSource.referrerPolicy;
        }
        if (self.AbortController) {
            this._abortController = new self.AbortController();
            params.signal = this._abortController.signal;
        }
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kConnecting;
        self.fetch(seekConfig.url, params).then(function (res) {
            if (_this._requestAbort) {
                _this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kIdle;
                res.body.cancel();
                return;
            }
            if (res.ok && (res.status >= 200 && res.status <= 299)) {
                if (res.url !== seekConfig.url) {
                    if (_this._onURLRedirect) {
                        var redirectedURL = _this._seekHandler.removeURLParameters(res.url);
                        _this._onURLRedirect(redirectedURL);
                    }
                }
                var lengthHeader = res.headers.get('Content-Length');
                if (lengthHeader != null) {
                    _this._contentLength = parseInt(lengthHeader);
                    if (_this._contentLength !== 0) {
                        if (_this._onContentLengthKnown) {
                            _this._onContentLengthKnown(_this._contentLength);
                        }
                    }
                }
                return _this._pump.call(_this, res.body.getReader());
            }
            else {
                _this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kError;
                if (_this._onError) {
                    _this._onError(_loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].HTTP_STATUS_CODE_INVALID, { code: res.status, msg: res.statusText });
                }
                else {
                    throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_3__["RuntimeException"]('FetchStreamLoader: Http code invalid, ' + res.status + ' ' + res.statusText);
                }
            }
        }).catch(function (e) {
            if (_this._abortController && _this._abortController.signal.aborted) {
                return;
            }
            _this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kError;
            if (_this._onError) {
                _this._onError(_loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].EXCEPTION, { code: -1, msg: e.message });
            }
            else {
                throw e;
            }
        });
    };
    FetchStreamLoader.prototype.abort = function () {
        this._requestAbort = true;
        if (this._status !== _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kBuffering || !_utils_browser_js__WEBPACK_IMPORTED_MODULE_1__["default"].chrome) {
            // Chrome may throw Exception-like things here, avoid using if is buffering
            if (this._abortController) {
                try {
                    this._abortController.abort();
                }
                catch (e) { }
            }
        }
    };
    FetchStreamLoader.prototype._pump = function (reader) {
        var _this = this;
        return reader.read().then(function (result) {
            if (result.done) {
                // First check received length
                if (_this._contentLength !== null && _this._receivedLength < _this._contentLength) {
                    // Report Early-EOF
                    _this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kError;
                    var type = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].EARLY_EOF;
                    var info = { code: -1, msg: 'Fetch stream meet Early-EOF' };
                    if (_this._onError) {
                        _this._onError(type, info);
                    }
                    else {
                        throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_3__["RuntimeException"](info.msg);
                    }
                }
                else {
                    // OK. Download complete
                    _this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kComplete;
                    if (_this._onComplete) {
                        _this._onComplete(_this._range.from, _this._range.from + _this._receivedLength - 1);
                    }
                }
            }
            else {
                if (_this._abortController && _this._abortController.signal.aborted) {
                    _this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kComplete;
                    return;
                }
                else if (_this._requestAbort === true) {
                    _this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kComplete;
                    return reader.cancel();
                }
                _this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kBuffering;
                var chunk = result.value.buffer;
                var byteStart = _this._range.from + _this._receivedLength;
                _this._receivedLength += chunk.byteLength;
                if (_this._onDataArrival) {
                    _this._onDataArrival(chunk, byteStart, _this._receivedLength);
                }
                _this._pump(reader);
            }
        }).catch(function (e) {
            if (_this._abortController && _this._abortController.signal.aborted) {
                _this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kComplete;
                return;
            }
            if (e.code === 11 && _utils_browser_js__WEBPACK_IMPORTED_MODULE_1__["default"].msedge) { // InvalidStateError on Microsoft Edge
                // Workaround: Edge may throw InvalidStateError after ReadableStreamReader.cancel() call
                // Ignore the unknown exception.
                // Related issue: https://developer.microsoft.com/en-us/microsoft-edge/platform/issues/11265202/
                return;
            }
            _this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kError;
            var type = 0;
            var info = null;
            if ((e.code === 19 || e.message === 'network error') && // NETWORK_ERR
                (_this._contentLength === null ||
                    (_this._contentLength !== null && _this._receivedLength < _this._contentLength))) {
                type = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].EARLY_EOF;
                info = { code: e.code, msg: 'Fetch stream meet Early-EOF' };
            }
            else {
                type = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].EXCEPTION;
                info = { code: e.code, msg: e.message };
            }
            if (_this._onError) {
                _this._onError(type, info);
            }
            else {
                throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_3__["RuntimeException"](info.msg);
            }
        });
    };
    return FetchStreamLoader;
}(_loader_js__WEBPACK_IMPORTED_MODULE_2__["BaseLoader"]));
/* harmony default export */ __webpack_exports__["default"] = (FetchStreamLoader);


/***/ }),

/***/ "./src/io/io-controller.js":
/*!*********************************!*\
  !*** ./src/io/io-controller.js ***!
  \*********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/* harmony import */ var _speed_sampler_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ./speed-sampler.js */ "./src/io/speed-sampler.js");
/* harmony import */ var _loader_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ./loader.js */ "./src/io/loader.js");
/* harmony import */ var _fetch_stream_loader_js__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ./fetch-stream-loader.js */ "./src/io/fetch-stream-loader.js");
/* harmony import */ var _xhr_moz_chunked_loader_js__WEBPACK_IMPORTED_MODULE_4__ = __webpack_require__(/*! ./xhr-moz-chunked-loader.js */ "./src/io/xhr-moz-chunked-loader.js");
/* harmony import */ var _xhr_msstream_loader_js__WEBPACK_IMPORTED_MODULE_5__ = __webpack_require__(/*! ./xhr-msstream-loader.js */ "./src/io/xhr-msstream-loader.js");
/* harmony import */ var _xhr_range_loader_js__WEBPACK_IMPORTED_MODULE_6__ = __webpack_require__(/*! ./xhr-range-loader.js */ "./src/io/xhr-range-loader.js");
/* harmony import */ var _websocket_loader_js__WEBPACK_IMPORTED_MODULE_7__ = __webpack_require__(/*! ./websocket-loader.js */ "./src/io/websocket-loader.js");
/* harmony import */ var _rtmp_loader_js__WEBPACK_IMPORTED_MODULE_8__ = __webpack_require__(/*! ./rtmp-loader.js */ "./src/io/rtmp-loader.js");
/* harmony import */ var _range_seek_handler_js__WEBPACK_IMPORTED_MODULE_9__ = __webpack_require__(/*! ./range-seek-handler.js */ "./src/io/range-seek-handler.js");
/* harmony import */ var _param_seek_handler_js__WEBPACK_IMPORTED_MODULE_10__ = __webpack_require__(/*! ./param-seek-handler.js */ "./src/io/param-seek-handler.js");
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_11__ = __webpack_require__(/*! ../utils/exception.js */ "./src/utils/exception.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */












/**
 * DataSource: {
 *     url: string,
 *     filesize: number,
 *     cors: boolean,
 *     withCredentials: boolean
 * }
 *
 */
// Manage IO Loaders
var IOController = /** @class */ (function () {
    function IOController(dataSource, config, extraData) {
        this.TAG = 'IOController';
        this._config = config;
        this._extraData = extraData;
        this._stashInitialSize = 64 * 1024; // default initial size: 64KB
        if (config.stashInitialSize != undefined && config.stashInitialSize > 0) {
            // apply from config
            this._stashInitialSize = config.stashInitialSize;
        }
        this._stashUsed = 0;
        this._stashSize = this._stashInitialSize;
        this._bufferSize = Math.max(this._stashSize, 1024 * 1024 * 3); // initial size: 3MB at least
        this._stashBuffer = new ArrayBuffer(this._bufferSize);
        this._stashByteStart = 0;
        this._enableStash = true;
        if (config.enableStashBuffer === false) {
            this._enableStash = false;
        }
        this._loader = null;
        this._loaderClass = null;
        this._seekHandler = null;
        this._dataSource = dataSource;
        this._isWebSocketURL = /wss?:\/\/(.+?)/.test(dataSource.url);
        this._isRTMPURL = /^rtmp:\/\/(.+?)/.test(dataSource.url);
        this._refTotalLength = dataSource.filesize ? dataSource.filesize : null;
        this._totalLength = this._refTotalLength;
        this._fullRequestFlag = false;
        this._currentRange = null;
        this._redirectedURL = null;
        this._speedNormalized = 0;
        this._speedSampler = new _speed_sampler_js__WEBPACK_IMPORTED_MODULE_1__["default"]();
        this._speedNormalizeList = [32, 64, 96, 128, 192, 256, 384, 512, 768, 1024, 1536, 2048, 3072, 4096];
        this._isEarlyEofReconnecting = false;
        this._paused = false;
        this._resumeFrom = 0;
        this._onDataArrival = null;
        this._onSeeked = null;
        this._onError = null;
        this._onComplete = null;
        this._onRedirect = null;
        this._onRecoveredEarlyEof = null;
        this._selectSeekHandler();
        this._selectLoader();
        this._createLoader();
    }
    IOController.prototype.destroy = function () {
        if (this._loader.isWorking()) {
            this._loader.abort();
        }
        this._loader.destroy();
        this._loader = null;
        this._loaderClass = null;
        this._dataSource = null;
        this._stashBuffer = null;
        this._stashUsed = this._stashSize = this._bufferSize = this._stashByteStart = 0;
        this._currentRange = null;
        this._speedSampler = null;
        this._isEarlyEofReconnecting = false;
        this._onDataArrival = null;
        this._onSeeked = null;
        this._onError = null;
        this._onComplete = null;
        this._onRedirect = null;
        this._onRecoveredEarlyEof = null;
        this._extraData = null;
    };
    IOController.prototype.isWorking = function () {
        return this._loader && this._loader.isWorking() && !this._paused;
    };
    IOController.prototype.isPaused = function () {
        return this._paused;
    };
    Object.defineProperty(IOController.prototype, "status", {
        get: function () {
            return this._loader.status;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(IOController.prototype, "extraData", {
        get: function () {
            return this._extraData;
        },
        set: function (data) {
            this._extraData = data;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(IOController.prototype, "onDataArrival", {
        // prototype: function onDataArrival(chunks: ArrayBuffer, byteStart: number): number
        get: function () {
            return this._onDataArrival;
        },
        set: function (callback) {
            this._onDataArrival = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(IOController.prototype, "onSeeked", {
        get: function () {
            return this._onSeeked;
        },
        set: function (callback) {
            this._onSeeked = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(IOController.prototype, "onError", {
        // prototype: function onError(type: number, info: {code: number, msg: string}): void
        get: function () {
            return this._onError;
        },
        set: function (callback) {
            this._onError = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(IOController.prototype, "onComplete", {
        get: function () {
            return this._onComplete;
        },
        set: function (callback) {
            this._onComplete = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(IOController.prototype, "onRedirect", {
        get: function () {
            return this._onRedirect;
        },
        set: function (callback) {
            this._onRedirect = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(IOController.prototype, "onRecoveredEarlyEof", {
        get: function () {
            return this._onRecoveredEarlyEof;
        },
        set: function (callback) {
            this._onRecoveredEarlyEof = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(IOController.prototype, "currentURL", {
        get: function () {
            return this._dataSource.url;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(IOController.prototype, "hasRedirect", {
        get: function () {
            return (this._redirectedURL != null || this._dataSource.redirectedURL != undefined);
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(IOController.prototype, "currentRedirectedURL", {
        get: function () {
            return this._redirectedURL || this._dataSource.redirectedURL;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(IOController.prototype, "currentSpeed", {
        // in KB/s
        get: function () {
            if (this._loaderClass === _xhr_range_loader_js__WEBPACK_IMPORTED_MODULE_6__["default"]) {
                // SpeedSampler is inaccuracy if loader is RangeLoader
                return this._loader.currentSpeed;
            }
            return this._speedSampler.lastSecondKBps;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(IOController.prototype, "loaderType", {
        get: function () {
            return this._loader.type;
        },
        enumerable: false,
        configurable: true
    });
    IOController.prototype._selectSeekHandler = function () {
        var config = this._config;
        if (config.seekType === 'range') {
            this._seekHandler = new _range_seek_handler_js__WEBPACK_IMPORTED_MODULE_9__["default"](this._config.rangeLoadZeroStart);
        }
        else if (config.seekType === 'param') {
            var paramStart = config.seekParamStart || 'bstart';
            var paramEnd = config.seekParamEnd || 'bend';
            this._seekHandler = new _param_seek_handler_js__WEBPACK_IMPORTED_MODULE_10__["default"](paramStart, paramEnd);
        }
        else if (config.seekType === 'custom') {
            if (typeof config.customSeekHandler !== 'function') {
                throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_11__["InvalidArgumentException"]('Custom seekType specified in config but invalid customSeekHandler!');
            }
            this._seekHandler = new config.customSeekHandler();
        }
        else {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_11__["InvalidArgumentException"]("Invalid seekType in config: ".concat(config.seekType));
        }
    };
    IOController.prototype._selectLoader = function () {
        if (this._config.customLoader != null) {
            this._loaderClass = this._config.customLoader;
        }
        else if (this._isRTMPURL) {
            this._loaderClass = _rtmp_loader_js__WEBPACK_IMPORTED_MODULE_8__["default"];
        }
        else if (this._isWebSocketURL) {
            this._loaderClass = _websocket_loader_js__WEBPACK_IMPORTED_MODULE_7__["default"];
        }
        else if (_fetch_stream_loader_js__WEBPACK_IMPORTED_MODULE_3__["default"].isSupported()) {
            this._loaderClass = _fetch_stream_loader_js__WEBPACK_IMPORTED_MODULE_3__["default"];
        }
        else if (_xhr_moz_chunked_loader_js__WEBPACK_IMPORTED_MODULE_4__["default"].isSupported()) {
            this._loaderClass = _xhr_moz_chunked_loader_js__WEBPACK_IMPORTED_MODULE_4__["default"];
        }
        else if (_xhr_range_loader_js__WEBPACK_IMPORTED_MODULE_6__["default"].isSupported()) {
            this._loaderClass = _xhr_range_loader_js__WEBPACK_IMPORTED_MODULE_6__["default"];
        }
        else {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_11__["RuntimeException"]('Your browser doesn\'t support xhr with arraybuffer responseType!');
        }
    };
    IOController.prototype._createLoader = function () {
        this._loader = new this._loaderClass(this._seekHandler, this._config);
        if (this._loader.needStashBuffer === false) {
            this._enableStash = false;
        }
        this._loader.onContentLengthKnown = this._onContentLengthKnown.bind(this);
        this._loader.onURLRedirect = this._onURLRedirect.bind(this);
        this._loader.onDataArrival = this._onLoaderChunkArrival.bind(this);
        this._loader.onComplete = this._onLoaderComplete.bind(this);
        this._loader.onError = this._onLoaderError.bind(this);
    };
    IOController.prototype.open = function (optionalFrom) {
        this._currentRange = { from: 0, to: -1 };
        if (optionalFrom) {
            this._currentRange.from = optionalFrom;
        }
        this._speedSampler.reset();
        if (!optionalFrom) {
            this._fullRequestFlag = true;
        }
        this._loader.open(this._dataSource, Object.assign({}, this._currentRange));
    };
    IOController.prototype.abort = function () {
        this._loader.abort();
        if (this._paused) {
            this._paused = false;
            this._resumeFrom = 0;
        }
    };
    IOController.prototype.pause = function () {
        if (this.isWorking()) {
            this._loader.abort();
            if (this._stashUsed !== 0) {
                this._resumeFrom = this._stashByteStart;
                this._currentRange.to = this._stashByteStart - 1;
            }
            else {
                this._resumeFrom = this._currentRange.to + 1;
            }
            this._stashUsed = 0;
            this._stashByteStart = 0;
            this._paused = true;
        }
    };
    IOController.prototype.resume = function () {
        if (this._paused) {
            this._paused = false;
            var bytes = this._resumeFrom;
            this._resumeFrom = 0;
            this._internalSeek(bytes, true);
        }
    };
    IOController.prototype.seek = function (bytes) {
        this._paused = false;
        this._stashUsed = 0;
        this._stashByteStart = 0;
        this._internalSeek(bytes, true);
    };
    /**
     * When seeking request is from media seeking, unconsumed stash data should be dropped
     * However, stash data shouldn't be dropped if seeking requested from http reconnection
     *
     * @dropUnconsumed: Ignore and discard all unconsumed data in stash buffer
     */
    IOController.prototype._internalSeek = function (bytes, dropUnconsumed) {
        if (this._loader.isWorking()) {
            this._loader.abort();
        }
        // dispatch & flush stash buffer before seek
        this._flushStashBuffer(dropUnconsumed);
        this._loader.destroy();
        this._loader = null;
        var requestRange = { from: bytes, to: -1 };
        this._currentRange = { from: requestRange.from, to: -1 };
        this._speedSampler.reset();
        this._stashSize = this._stashInitialSize;
        this._createLoader();
        this._loader.open(this._dataSource, requestRange);
        if (this._onSeeked) {
            this._onSeeked();
        }
    };
    IOController.prototype.updateUrl = function (url) {
        if (!url || typeof url !== 'string' || url.length === 0) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_11__["InvalidArgumentException"]('Url must be a non-empty string!');
        }
        this._dataSource.url = url;
        // TODO: replace with new url
    };
    IOController.prototype._expandBuffer = function (expectedBytes) {
        var bufferNewSize = this._stashSize;
        while (bufferNewSize + 1024 * 1024 * 1 < expectedBytes) {
            bufferNewSize *= 2;
        }
        bufferNewSize += 1024 * 1024 * 1; // bufferSize = stashSize + 1MB
        if (bufferNewSize === this._bufferSize) {
            return;
        }
        var newBuffer = new ArrayBuffer(bufferNewSize);
        if (this._stashUsed > 0) { // copy existing data into new buffer
            var stashOldArray = new Uint8Array(this._stashBuffer, 0, this._stashUsed);
            var stashNewArray = new Uint8Array(newBuffer, 0, bufferNewSize);
            stashNewArray.set(stashOldArray, 0);
        }
        this._stashBuffer = newBuffer;
        this._bufferSize = bufferNewSize;
    };
    IOController.prototype._normalizeSpeed = function (input) {
        var list = this._speedNormalizeList;
        var last = list.length - 1;
        var mid = 0;
        var lbound = 0;
        var ubound = last;
        if (input < list[0]) {
            return list[0];
        }
        // binary search
        while (lbound <= ubound) {
            mid = lbound + Math.floor((ubound - lbound) / 2);
            if (mid === last || (input >= list[mid] && input < list[mid + 1])) {
                return list[mid];
            }
            else if (list[mid] < input) {
                lbound = mid + 1;
            }
            else {
                ubound = mid - 1;
            }
        }
    };
    IOController.prototype._adjustStashSize = function (normalized) {
        var stashSizeKB = 0;
        if (this._config.isLive) {
            // live stream: always use 1/8 normalized speed for size of stashSizeKB
            stashSizeKB = normalized / 8;
        }
        else {
            if (normalized < 512) {
                stashSizeKB = normalized;
            }
            else if (normalized >= 512 && normalized <= 1024) {
                stashSizeKB = Math.floor(normalized * 1.5);
            }
            else {
                stashSizeKB = normalized * 2;
            }
        }
        if (stashSizeKB > 8192) {
            stashSizeKB = 8192;
        }
        var bufferSize = stashSizeKB * 1024 + 1024 * 1024 * 1; // stashSize + 1MB
        if (this._bufferSize < bufferSize) {
            this._expandBuffer(bufferSize);
        }
        this._stashSize = stashSizeKB * 1024;
    };
    IOController.prototype._dispatchChunks = function (chunks, byteStart) {
        this._currentRange.to = byteStart + chunks.byteLength - 1;
        return this._onDataArrival(chunks, byteStart);
    };
    IOController.prototype._onURLRedirect = function (redirectedURL) {
        this._redirectedURL = redirectedURL;
        if (this._onRedirect) {
            this._onRedirect(redirectedURL);
        }
    };
    IOController.prototype._onContentLengthKnown = function (contentLength) {
        if (contentLength && this._fullRequestFlag) {
            this._totalLength = contentLength;
            this._fullRequestFlag = false;
        }
    };
    IOController.prototype._onLoaderChunkArrival = function (chunk, byteStart, receivedLength) {
        if (!this._onDataArrival) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_11__["IllegalStateException"]('IOController: No existing consumer (onDataArrival) callback!');
        }
        if (this._paused) {
            return;
        }
        if (this._isEarlyEofReconnecting) {
            // Auto-reconnect for EarlyEof succeed, notify to upper-layer by callback
            this._isEarlyEofReconnecting = false;
            if (this._onRecoveredEarlyEof) {
                this._onRecoveredEarlyEof();
            }
        }
        this._speedSampler.addBytes(chunk.byteLength);
        // adjust stash buffer size according to network speed dynamically
        var KBps = this._speedSampler.lastSecondKBps;
        if (KBps !== 0) {
            var normalized = this._normalizeSpeed(KBps);
            if (this._speedNormalized !== normalized) {
                this._speedNormalized = normalized;
                this._adjustStashSize(normalized);
            }
        }
        if (!this._enableStash) { // disable stash
            if (this._stashUsed === 0) {
                // dispatch chunk directly to consumer;
                // check ret value (consumed bytes) and stash unconsumed to stashBuffer
                var consumed = this._dispatchChunks(chunk, byteStart);
                if (consumed < chunk.byteLength) { // unconsumed data remain.
                    var remain = chunk.byteLength - consumed;
                    if (remain > this._bufferSize) {
                        this._expandBuffer(remain);
                    }
                    var stashArray = new Uint8Array(this._stashBuffer, 0, this._bufferSize);
                    stashArray.set(new Uint8Array(chunk, consumed), 0);
                    this._stashUsed += remain;
                    this._stashByteStart = byteStart + consumed;
                }
            }
            else {
                // else: Merge chunk into stashBuffer, and dispatch stashBuffer to consumer.
                if (this._stashUsed + chunk.byteLength > this._bufferSize) {
                    this._expandBuffer(this._stashUsed + chunk.byteLength);
                }
                var stashArray = new Uint8Array(this._stashBuffer, 0, this._bufferSize);
                stashArray.set(new Uint8Array(chunk), this._stashUsed);
                this._stashUsed += chunk.byteLength;
                var consumed = this._dispatchChunks(this._stashBuffer.slice(0, this._stashUsed), this._stashByteStart);
                if (consumed < this._stashUsed && consumed > 0) { // unconsumed data remain
                    var remainArray = new Uint8Array(this._stashBuffer, consumed);
                    stashArray.set(remainArray, 0);
                }
                this._stashUsed -= consumed;
                this._stashByteStart += consumed;
            }
        }
        else { // enable stash
            if (this._stashUsed === 0 && this._stashByteStart === 0) { // seeked? or init chunk?
                // This is the first chunk after seek action
                this._stashByteStart = byteStart;
            }
            if (this._stashUsed + chunk.byteLength <= this._stashSize) {
                // just stash
                var stashArray = new Uint8Array(this._stashBuffer, 0, this._stashSize);
                stashArray.set(new Uint8Array(chunk), this._stashUsed);
                this._stashUsed += chunk.byteLength;
            }
            else { // stashUsed + chunkSize > stashSize, size limit exceeded
                var stashArray = new Uint8Array(this._stashBuffer, 0, this._bufferSize);
                if (this._stashUsed > 0) { // There're stash datas in buffer
                    // dispatch the whole stashBuffer, and stash remain data
                    // then append chunk to stashBuffer (stash)
                    var buffer = this._stashBuffer.slice(0, this._stashUsed);
                    var consumed = this._dispatchChunks(buffer, this._stashByteStart);
                    if (consumed < buffer.byteLength) {
                        if (consumed > 0) {
                            var remainArray = new Uint8Array(buffer, consumed);
                            stashArray.set(remainArray, 0);
                            this._stashUsed = remainArray.byteLength;
                            this._stashByteStart += consumed;
                        }
                    }
                    else {
                        this._stashUsed = 0;
                        this._stashByteStart += consumed;
                    }
                    if (this._stashUsed + chunk.byteLength > this._bufferSize) {
                        this._expandBuffer(this._stashUsed + chunk.byteLength);
                        stashArray = new Uint8Array(this._stashBuffer, 0, this._bufferSize);
                    }
                    stashArray.set(new Uint8Array(chunk), this._stashUsed);
                    this._stashUsed += chunk.byteLength;
                }
                else { // stash buffer empty, but chunkSize > stashSize (oh, holy shit)
                    // dispatch chunk directly and stash remain data
                    var consumed = this._dispatchChunks(chunk, byteStart);
                    if (consumed < chunk.byteLength) {
                        var remain = chunk.byteLength - consumed;
                        if (remain > this._bufferSize) {
                            this._expandBuffer(remain);
                            stashArray = new Uint8Array(this._stashBuffer, 0, this._bufferSize);
                        }
                        stashArray.set(new Uint8Array(chunk, consumed), 0);
                        this._stashUsed += remain;
                        this._stashByteStart = byteStart + consumed;
                    }
                }
            }
        }
    };
    IOController.prototype._flushStashBuffer = function (dropUnconsumed) {
        if (this._stashUsed > 0) {
            var buffer = this._stashBuffer.slice(0, this._stashUsed);
            var consumed = this._dispatchChunks(buffer, this._stashByteStart);
            var remain = buffer.byteLength - consumed;
            if (consumed < buffer.byteLength) {
                if (dropUnconsumed) {
                    _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "".concat(remain, " bytes unconsumed data remain when flush buffer, dropped"));
                }
                else {
                    if (consumed > 0) {
                        var stashArray = new Uint8Array(this._stashBuffer, 0, this._bufferSize);
                        var remainArray = new Uint8Array(buffer, consumed);
                        stashArray.set(remainArray, 0);
                        this._stashUsed = remainArray.byteLength;
                        this._stashByteStart += consumed;
                    }
                    return 0;
                }
            }
            this._stashUsed = 0;
            this._stashByteStart = 0;
            return remain;
        }
        return 0;
    };
    IOController.prototype._onLoaderComplete = function (from, to) {
        // Force-flush stash buffer, and drop unconsumed data
        this._flushStashBuffer(true);
        if (this._onComplete) {
            this._onComplete(this._extraData);
        }
    };
    IOController.prototype._onLoaderError = function (type, data) {
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "Loader error, code = ".concat(data.code, ", msg = ").concat(data.msg));
        this._flushStashBuffer(false);
        if (this._isEarlyEofReconnecting) {
            // Auto-reconnect for EarlyEof failed, throw UnrecoverableEarlyEof error to upper-layer
            this._isEarlyEofReconnecting = false;
            type = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].UNRECOVERABLE_EARLY_EOF;
        }
        switch (type) {
            case _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].EARLY_EOF: {
                if (!this._config.isLive) {
                    // Do internal http reconnect if not live stream
                    if (this._totalLength) {
                        var nextFrom = this._currentRange.to + 1;
                        if (nextFrom < this._totalLength) {
                            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Connection lost, trying reconnect...');
                            this._isEarlyEofReconnecting = true;
                            this._internalSeek(nextFrom, false);
                        }
                        return;
                    }
                    // else: We don't know totalLength, throw UnrecoverableEarlyEof
                }
                // live stream: throw UnrecoverableEarlyEof error to upper-layer
                type = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].UNRECOVERABLE_EARLY_EOF;
                break;
            }
            case _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].UNRECOVERABLE_EARLY_EOF:
            case _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].CONNECTING_TIMEOUT:
            case _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].HTTP_STATUS_CODE_INVALID:
            case _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].EXCEPTION:
                break;
        }
        if (this._onError) {
            this._onError(type, data);
        }
        else {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_11__["RuntimeException"]('IOException: ' + data.msg);
        }
    };
    return IOController;
}());
/* harmony default export */ __webpack_exports__["default"] = (IOController);


/***/ }),

/***/ "./src/io/loader.js":
/*!**************************!*\
  !*** ./src/io/loader.js ***!
  \**************************/
/*! exports provided: LoaderStatus, LoaderErrors, BaseLoader */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "LoaderStatus", function() { return LoaderStatus; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "LoaderErrors", function() { return LoaderErrors; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "BaseLoader", function() { return BaseLoader; });
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/exception.js */ "./src/utils/exception.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

var LoaderStatus = {
    kIdle: 0,
    kConnecting: 1,
    kBuffering: 2,
    kError: 3,
    kComplete: 4
};
var LoaderErrors = {
    OK: 'OK',
    EXCEPTION: 'Exception',
    HTTP_STATUS_CODE_INVALID: 'HttpStatusCodeInvalid',
    CONNECTING_TIMEOUT: 'ConnectingTimeout',
    EARLY_EOF: 'EarlyEof',
    UNRECOVERABLE_EARLY_EOF: 'UnrecoverableEarlyEof'
};
/* Loader has callbacks which have following prototypes:
 *     function onContentLengthKnown(contentLength: number): void
 *     function onURLRedirect(url: string): void
 *     function onDataArrival(chunk: ArrayBuffer, byteStart: number, receivedLength: number): void
 *     function onError(errorType: number, errorInfo: {code: number, msg: string}): void
 *     function onComplete(rangeFrom: number, rangeTo: number): void
 */
var BaseLoader = /** @class */ (function () {
    function BaseLoader(typeName) {
        this._type = typeName || 'undefined';
        this._status = LoaderStatus.kIdle;
        this._needStash = false;
        // callbacks
        this._onContentLengthKnown = null;
        this._onURLRedirect = null;
        this._onDataArrival = null;
        this._onError = null;
        this._onComplete = null;
    }
    BaseLoader.prototype.destroy = function () {
        this._status = LoaderStatus.kIdle;
        this._onContentLengthKnown = null;
        this._onURLRedirect = null;
        this._onDataArrival = null;
        this._onError = null;
        this._onComplete = null;
    };
    BaseLoader.prototype.isWorking = function () {
        return this._status === LoaderStatus.kConnecting || this._status === LoaderStatus.kBuffering;
    };
    Object.defineProperty(BaseLoader.prototype, "type", {
        get: function () {
            return this._type;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(BaseLoader.prototype, "status", {
        get: function () {
            return this._status;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(BaseLoader.prototype, "needStashBuffer", {
        get: function () {
            return this._needStash;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(BaseLoader.prototype, "onContentLengthKnown", {
        get: function () {
            return this._onContentLengthKnown;
        },
        set: function (callback) {
            this._onContentLengthKnown = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(BaseLoader.prototype, "onURLRedirect", {
        get: function () {
            return this._onURLRedirect;
        },
        set: function (callback) {
            this._onURLRedirect = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(BaseLoader.prototype, "onDataArrival", {
        get: function () {
            return this._onDataArrival;
        },
        set: function (callback) {
            this._onDataArrival = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(BaseLoader.prototype, "onError", {
        get: function () {
            return this._onError;
        },
        set: function (callback) {
            this._onError = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(BaseLoader.prototype, "onComplete", {
        get: function () {
            return this._onComplete;
        },
        set: function (callback) {
            this._onComplete = callback;
        },
        enumerable: false,
        configurable: true
    });
    // pure virtual
    BaseLoader.prototype.open = function (dataSource, range) {
        throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_0__["NotImplementedException"]('Unimplemented abstract function!');
    };
    BaseLoader.prototype.abort = function () {
        throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_0__["NotImplementedException"]('Unimplemented abstract function!');
    };
    return BaseLoader;
}());



/***/ }),

/***/ "./src/io/param-seek-handler.js":
/*!**************************************!*\
  !*** ./src/io/param-seek-handler.js ***!
  \**************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var ParamSeekHandler = /** @class */ (function () {
    function ParamSeekHandler(paramStart, paramEnd) {
        this._startName = paramStart;
        this._endName = paramEnd;
    }
    ParamSeekHandler.prototype.getConfig = function (baseUrl, range) {
        var url = baseUrl;
        if (range.from !== 0 || range.to !== -1) {
            var needAnd = true;
            if (url.indexOf('?') === -1) {
                url += '?';
                needAnd = false;
            }
            if (needAnd) {
                url += '&';
            }
            url += "".concat(this._startName, "=").concat(range.from.toString());
            if (range.to !== -1) {
                url += "&".concat(this._endName, "=").concat(range.to.toString());
            }
        }
        return {
            url: url,
            headers: {}
        };
    };
    ParamSeekHandler.prototype.removeURLParameters = function (seekedURL) {
        var baseURL = seekedURL.split('?')[0];
        var params = undefined;
        var queryIndex = seekedURL.indexOf('?');
        if (queryIndex !== -1) {
            params = seekedURL.substring(queryIndex + 1);
        }
        var resultParams = '';
        if (params != undefined && params.length > 0) {
            var pairs = params.split('&');
            for (var i = 0; i < pairs.length; i++) {
                var pair = pairs[i].split('=');
                var requireAnd = (i > 0);
                if (pair[0] !== this._startName && pair[0] !== this._endName) {
                    if (requireAnd) {
                        resultParams += '&';
                    }
                    resultParams += pairs[i];
                }
            }
        }
        return (resultParams.length === 0) ? baseURL : baseURL + '?' + resultParams;
    };
    return ParamSeekHandler;
}());
/* harmony default export */ __webpack_exports__["default"] = (ParamSeekHandler);


/***/ }),

/***/ "./src/io/range-seek-handler.js":
/*!**************************************!*\
  !*** ./src/io/range-seek-handler.js ***!
  \**************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var RangeSeekHandler = /** @class */ (function () {
    function RangeSeekHandler(zeroStart) {
        this._zeroStart = zeroStart || false;
    }
    RangeSeekHandler.prototype.getConfig = function (url, range) {
        var headers = {};
        if (range.from !== 0 || range.to !== -1) {
            var param = void 0;
            if (range.to !== -1) {
                param = "bytes=".concat(range.from.toString(), "-").concat(range.to.toString());
            }
            else {
                param = "bytes=".concat(range.from.toString(), "-");
            }
            headers['Range'] = param;
        }
        else if (this._zeroStart) {
            headers['Range'] = 'bytes=0-';
        }
        return {
            url: url,
            headers: headers
        };
    };
    RangeSeekHandler.prototype.removeURLParameters = function (seekedURL) {
        return seekedURL;
    };
    return RangeSeekHandler;
}());
/* harmony default export */ __webpack_exports__["default"] = (RangeSeekHandler);


/***/ }),

/***/ "./src/io/rtmp-loader.js":
/*!*******************************!*\
  !*** ./src/io/rtmp-loader.js ***!
  \*******************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* WEBPACK VAR INJECTION */(function(Buffer) {/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/* harmony import */ var _loader_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ./loader.js */ "./src/io/loader.js");
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ../utils/exception.js */ "./src/utils/exception.js");
/* harmony import */ var _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ./rtmp-protocol.js */ "./src/io/rtmp-protocol.js");
/* harmony import */ var _amf_encoder_js__WEBPACK_IMPORTED_MODULE_4__ = __webpack_require__(/*! ./amf-encoder.js */ "./src/io/amf-encoder.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var __extends = (undefined && undefined.__extends) || (function () {
    var extendStatics = function (d, b) {
        extendStatics = Object.setPrototypeOf ||
            ({ __proto__: [] } instanceof Array && function (d, b) { d.__proto__ = b; }) ||
            function (d, b) { for (var p in b) if (Object.prototype.hasOwnProperty.call(b, p)) d[p] = b[p]; };
        return extendStatics(d, b);
    };
    return function (d, b) {
        if (typeof b !== "function" && b !== null)
            throw new TypeError("Class extends value " + String(b) + " is not a constructor or null");
        extendStatics(d, b);
        function __() { this.constructor = d; }
        d.prototype = b === null ? Object.create(b) : (__.prototype = b.prototype, new __());
    };
})();





// RTMP Loader for live streaming (Node.js only)
var RTMPLoader = /** @class */ (function (_super) {
    __extends(RTMPLoader, _super);
    function RTMPLoader() {
        var _this = _super.call(this, 'rtmp-loader') || this;
        _this.TAG = 'RTMPLoader';
        // RTMP needs stash buffer for smooth data delivery
        _this._needStash = true;
        // Connection state
        _this._socket = null;
        _this._requestAbort = false;
        _this._receivedLength = 0;
        // RTMP protocol state
        _this._handshakeState = 'none'; // none, c0c1_sent, c2_sent, done
        _this._commandState = 'none'; // none, connect_sent, createStream_sent, play_sent, playing
        _this._streamId = null;
        // Chunk processing
        _this._chunkAggregator = new _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPChunkAggregator"]();
        _this._receiveBuffer = Buffer.allocUnsafe(0);
        // Connection info
        _this._rtmpUrl = null;
        return _this;
    }
    RTMPLoader.isSupported = function () {
        // Check if we're in Node.js environment
        try {
            return typeof window === 'undefined' && "function" !== 'undefined';
        }
        catch (e) {
            return false;
        }
    };
    RTMPLoader.prototype.destroy = function () {
        if (this._socket) {
            this.abort();
        }
        _super.prototype.destroy.call(this);
    };
    RTMPLoader.prototype.open = function (dataSource) {
        if (!RTMPLoader.isSupported()) {
            var error = 'RTMP is only supported in Node.js environment. For browser support, use WebSocket proxy.';
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, error);
            if (this._onError) {
                this._onError(_loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderErrors"].EXCEPTION, { code: -1, msg: error });
            }
            else {
                throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["RuntimeException"](error);
            }
            return;
        }
        try {
            // Parse RTMP URL
            this._rtmpUrl = Object(_rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["parseRTMPURL"])(dataSource.url);
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Connecting to RTMP: ".concat(this._rtmpUrl.tcUrl, "/").concat(this._rtmpUrl.stream));
            // Import Node.js 'net' module dynamically
            var net = __webpack_require__(!(function webpackMissingModule() { var e = new Error("Cannot find module 'net'"); e.code = 'MODULE_NOT_FOUND'; throw e; }()));
            // Create TCP connection
            this._socket = net.createConnection({
                host: this._rtmpUrl.host,
                port: this._rtmpUrl.port
            });
            this._socket.on('connect', this._onSocketConnect.bind(this));
            this._socket.on('data', this._onSocketData.bind(this));
            this._socket.on('end', this._onSocketEnd.bind(this));
            this._socket.on('error', this._onSocketError.bind(this));
            this._socket.on('close', this._onSocketClose.bind(this));
            this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kConnecting;
        }
        catch (e) {
            this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kError;
            var info = { code: e.code || -1, msg: e.message };
            if (this._onError) {
                this._onError(_loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderErrors"].EXCEPTION, info);
            }
            else {
                throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["RuntimeException"](info.msg);
            }
        }
    };
    RTMPLoader.prototype.abort = function () {
        if (this._socket) {
            this._requestAbort = true;
            this._socket.destroy();
        }
        this._socket = null;
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kComplete;
    };
    RTMPLoader.prototype._onSocketConnect = function () {
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'TCP connection established, starting RTMP handshake');
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kConnecting;
        // Start RTMP handshake: send C0 + C1
        this._sendHandshakeC0C1();
    };
    RTMPLoader.prototype._onSocketData = function (data) {
        if (this._requestAbort) {
            return;
        }
        // Append new data to receive buffer
        this._receiveBuffer = Buffer.concat([this._receiveBuffer, data]);
        // Process data based on current state
        if (this._handshakeState !== 'done') {
            this._processHandshake();
        }
        else {
            this._processChunks();
        }
    };
    RTMPLoader.prototype._onSocketEnd = function () {
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Socket ended');
        if (this._requestAbort) {
            this._requestAbort = false;
            return;
        }
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kComplete;
        if (this._onComplete) {
            this._onComplete(0, this._receivedLength - 1);
        }
    };
    RTMPLoader.prototype._onSocketError = function (error) {
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "Socket error: ".concat(error.message));
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kError;
        var info = {
            code: error.code || -1,
            msg: error.message
        };
        if (this._onError) {
            this._onError(_loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderErrors"].EXCEPTION, info);
        }
        else {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["RuntimeException"](info.msg);
        }
    };
    RTMPLoader.prototype._onSocketClose = function (hadError) {
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Socket closed, hadError=".concat(hadError));
        if (!this._requestAbort && !hadError) {
            this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kComplete;
            if (this._onComplete) {
                this._onComplete(0, this._receivedLength - 1);
            }
        }
    };
    // ==================== RTMP Handshake ====================
    RTMPLoader.prototype._sendHandshakeC0C1 = function () {
        // C0: 1 byte protocol version (0x03)
        // C1: 1536 bytes (timestamp + zero + random)
        var c0c1 = Buffer.allocUnsafe(1 + _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPConstants"].HANDSHAKE_SIZE);
        // C0
        c0c1[0] = _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPConstants"].PROTOCOL_VERSION;
        // C1: timestamp (4 bytes) + zero (4 bytes) + random (1528 bytes)
        var timestamp = Date.now();
        c0c1.writeUInt32BE(timestamp, 1);
        c0c1.writeUInt32BE(0, 5); // Zero
        // Fill with random data
        for (var i = 9; i < 1 + _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPConstants"].HANDSHAKE_SIZE; i++) {
            c0c1[i] = Math.floor(Math.random() * 256);
        }
        this._socket.write(c0c1);
        this._handshakeState = 'c0c1_sent';
        this._c1Data = c0c1.slice(1); // Save C1 for C2 echo
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Sent C0+C1');
    };
    RTMPLoader.prototype._sendHandshakeC2 = function (s1Data) {
        // C2: Echo of S1
        this._socket.write(s1Data);
        this._handshakeState = 'c2_sent';
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Sent C2, handshake complete');
        // Handshake done, send connect command
        this._handshakeState = 'done';
        this._sendConnectCommand();
    };
    RTMPLoader.prototype._processHandshake = function () {
        if (this._handshakeState === 'c0c1_sent') {
            // Wait for S0 + S1 + S2 (1 + 1536 + 1536 = 3073 bytes)
            var requiredBytes = 1 + _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPConstants"].HANDSHAKE_SIZE * 2;
            if (this._receiveBuffer.length >= requiredBytes) {
                // Verify S0
                var s0 = this._receiveBuffer[0];
                if (s0 !== _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPConstants"].PROTOCOL_VERSION) {
                    this._onSocketError(new Error("Invalid S0: ".concat(s0, ", expected ").concat(_rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPConstants"].PROTOCOL_VERSION)));
                    return;
                }
                // Extract S1
                var s1 = this._receiveBuffer.slice(1, 1 + _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPConstants"].HANDSHAKE_SIZE);
                // Extract S2 (should be echo of C1, but we don't validate it)
                // const s2 = this._receiveBuffer.slice(1 + RTMPConstants.HANDSHAKE_SIZE, requiredBytes);
                // Remove processed bytes from buffer
                this._receiveBuffer = this._receiveBuffer.slice(requiredBytes);
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Received S0+S1+S2');
                // Send C2
                this._sendHandshakeC2(s1);
            }
        }
        else if (this._handshakeState === 'c2_sent') {
            // Already transitioned to 'done' in _sendHandshakeC2
            // Process any remaining data as chunks
            if (this._receiveBuffer.length > 0) {
                this._processChunks();
            }
        }
    };
    // ==================== RTMP Commands ====================
    RTMPLoader.prototype._sendConnectCommand = function () {
        var connectCmd = Object(_amf_encoder_js__WEBPACK_IMPORTED_MODULE_4__["createConnectCommand"])(this._rtmpUrl.app, this._rtmpUrl.tcUrl);
        // Send as RTMP chunk on chunk stream ID 3, message stream ID 0
        this._sendRTMPMessage(3, 0, _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPMessageType"].COMMAND_AMF0, connectCmd);
        this._commandState = 'connect_sent';
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Sent connect() command');
    };
    RTMPLoader.prototype._sendCreateStreamCommand = function () {
        var createStreamCmd = Object(_amf_encoder_js__WEBPACK_IMPORTED_MODULE_4__["createCreateStreamCommand"])();
        // Send on chunk stream ID 3, message stream ID 0
        this._sendRTMPMessage(3, 0, _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPMessageType"].COMMAND_AMF0, createStreamCmd);
        this._commandState = 'createStream_sent';
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Sent createStream() command');
    };
    RTMPLoader.prototype._sendPlayCommand = function () {
        var playCmd = Object(_amf_encoder_js__WEBPACK_IMPORTED_MODULE_4__["createPlayCommand"])(this._rtmpUrl.stream);
        // Send on chunk stream ID 8, message stream ID = streamId
        this._sendRTMPMessage(8, this._streamId, _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPMessageType"].COMMAND_AMF0, playCmd);
        this._commandState = 'play_sent';
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Sent play(".concat(this._rtmpUrl.stream, ") command"));
        // Set buffer length (optional, but recommended for live streams)
        this._sendSetBufferLength(this._streamId, 3000); // 3 seconds
    };
    RTMPLoader.prototype._sendSetBufferLength = function (streamId, bufferLength) {
        // User control message: SetBufferLength (type 3)
        var buffer = Buffer.allocUnsafe(10);
        buffer.writeUInt16BE(3, 0); // Event type: SetBufferLength
        buffer.writeUInt32BE(streamId, 2);
        buffer.writeUInt32BE(bufferLength, 6);
        this._sendRTMPMessage(2, 0, _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPMessageType"].USER_CONTROL, buffer);
    };
    RTMPLoader.prototype._sendRTMPMessage = function (chunkStreamId, messageStreamId, messageTypeId, data) {
        var timestamp = 0;
        var messageLength = data.length;
        // For now, use Type 0 (full header) for simplicity
        // Basic header: fmt=0, csid=chunkStreamId
        var basicHeader;
        if (chunkStreamId <= 63) {
            basicHeader = Buffer.from([(0 << 6) | chunkStreamId]);
        }
        else if (chunkStreamId <= 319) {
            basicHeader = Buffer.from([0, chunkStreamId - 64]);
        }
        else {
            basicHeader = Buffer.from([1, (chunkStreamId - 64) & 0xFF, (chunkStreamId - 64) >> 8]);
        }
        // Message header (Type 0: 11 bytes)
        var messageHeader = Buffer.allocUnsafe(11);
        messageHeader[0] = (timestamp >> 16) & 0xFF;
        messageHeader[1] = (timestamp >> 8) & 0xFF;
        messageHeader[2] = timestamp & 0xFF;
        messageHeader[3] = (messageLength >> 16) & 0xFF;
        messageHeader[4] = (messageLength >> 8) & 0xFF;
        messageHeader[5] = messageLength & 0xFF;
        messageHeader[6] = messageTypeId;
        messageHeader[7] = messageStreamId & 0xFF;
        messageHeader[8] = (messageStreamId >> 8) & 0xFF;
        messageHeader[9] = (messageStreamId >> 16) & 0xFF;
        messageHeader[10] = (messageStreamId >> 24) & 0xFF;
        // Combine header and data
        var chunk = Buffer.concat([basicHeader, messageHeader, data]);
        this._socket.write(chunk);
    };
    // ==================== RTMP Chunk Processing ====================
    RTMPLoader.prototype._processChunks = function () {
        var offset = 0;
        while (offset < this._receiveBuffer.length) {
            // Try to parse basic header
            var basicHeader = _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPChunkHeader"].parseBasicHeader(this._receiveBuffer, offset);
            if (!basicHeader) {
                break; // Not enough data
            }
            offset += basicHeader.bytesRead;
            // Try to parse message header
            var result = _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPChunkHeader"].parseMessageHeader(this._receiveBuffer, offset, basicHeader, this._chunkAggregator);
            if (!result) {
                // Not enough data, restore offset and wait for more
                offset -= basicHeader.bytesRead;
                break;
            }
            var header = result.header, bytesRead = result.bytesRead;
            offset += bytesRead;
            // Calculate chunk data size (min of remaining message size and chunk size)
            var remainingMessageSize = header.messageLength;
            var chunkDataSize = Math.min(remainingMessageSize, this._chunkAggregator.chunkSize);
            // Check if we have enough data for this chunk
            if (offset + chunkDataSize > this._receiveBuffer.length) {
                // Not enough data, restore offset and wait
                offset -= (basicHeader.bytesRead + bytesRead);
                break;
            }
            // Extract chunk data
            var chunkData = this._receiveBuffer.slice(offset, offset + chunkDataSize);
            offset += chunkDataSize;
            // Add chunk to aggregator
            var completeMessage = this._chunkAggregator.addChunk(header, chunkData);
            if (completeMessage) {
                this._handleRTMPMessage(completeMessage);
            }
        }
        // Remove processed bytes from buffer
        if (offset > 0) {
            this._receiveBuffer = this._receiveBuffer.slice(offset);
        }
    };
    RTMPLoader.prototype._handleRTMPMessage = function (message) {
        var header = message.header, data = message.data;
        var messageType = header.messageTypeId;
        switch (messageType) {
            case _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPMessageType"].SET_CHUNK_SIZE:
                this._handleSetChunkSize(data);
                break;
            case _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPMessageType"].WINDOW_ACK_SIZE:
                this._handleWindowAckSize(data);
                break;
            case _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPMessageType"].SET_PEER_BANDWIDTH:
                this._handleSetPeerBandwidth(data);
                break;
            case _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPMessageType"].USER_CONTROL:
                this._handleUserControl(data);
                break;
            case _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPMessageType"].COMMAND_AMF0:
                this._handleCommandMessage(data);
                break;
            case _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPMessageType"].AUDIO:
            case _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPMessageType"].VIDEO:
            case _rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["RTMPMessageType"].DATA_AMF0:
                this._handleMediaData(message);
                break;
            default:
                _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Unhandled message type: ".concat(messageType));
                break;
        }
    };
    RTMPLoader.prototype._handleSetChunkSize = function (data) {
        var chunkSize = data.readUInt32BE(0);
        this._chunkAggregator.setChunkSize(chunkSize);
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Chunk size set to: ".concat(chunkSize));
    };
    RTMPLoader.prototype._handleWindowAckSize = function (data) {
        var windowSize = data.readUInt32BE(0);
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Window ack size: ".concat(windowSize));
    };
    RTMPLoader.prototype._handleSetPeerBandwidth = function (data) {
        var windowSize = data.readUInt32BE(0);
        var limitType = data[4];
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Set peer bandwidth: ".concat(windowSize, ", limit type: ").concat(limitType));
    };
    RTMPLoader.prototype._handleUserControl = function (data) {
        var eventType = data.readUInt16BE(0);
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "User control event: ".concat(eventType));
        // Event type 0: StreamBegin
        if (eventType === 0) {
            this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kBuffering;
        }
    };
    RTMPLoader.prototype._handleCommandMessage = function (data) {
        // Parse AMF command (we need AMF decoder from existing amf-parser.js)
        var AMF = __webpack_require__(/*! ../demux/amf-parser.js */ "./src/demux/amf-parser.js").default;
        try {
            var offset = 0;
            var commandName = AMF.parseValue(data.buffer, data.byteOffset + offset, data.length - offset);
            offset += commandName.size;
            var transactionId = AMF.parseValue(data.buffer, data.byteOffset + offset, data.length - offset);
            offset += transactionId.size;
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "Received command: ".concat(commandName.data, ", transaction: ").concat(transactionId.data));
            if (commandName.data === '_result') {
                if (this._commandState === 'connect_sent') {
                    // connect() succeeded
                    _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'connect() succeeded');
                    this._sendCreateStreamCommand();
                }
                else if (this._commandState === 'createStream_sent') {
                    // createStream() succeeded, parse stream ID
                    offset += AMF.parseValue(data.buffer, data.byteOffset + offset, data.length - offset).size; // Skip command object
                    var streamIdValue = AMF.parseValue(data.buffer, data.byteOffset + offset, data.length - offset);
                    this._streamId = streamIdValue.data;
                    _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "createStream() succeeded, streamId=".concat(this._streamId));
                    this._sendPlayCommand();
                }
            }
            else if (commandName.data === 'onStatus') {
                // Parse status object
                offset += AMF.parseValue(data.buffer, data.byteOffset + offset, data.length - offset).size; // Skip command object
                if (offset < data.length) {
                    var statusObj = AMF.parseValue(data.buffer, data.byteOffset + offset, data.length - offset);
                    _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "onStatus: ".concat(JSON.stringify(statusObj.data)));
                    if (statusObj.data && statusObj.data.code === 'NetStream.Play.Start') {
                        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Stream playback started');
                        this._commandState = 'playing';
                    }
                }
            }
        }
        catch (e) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, "Failed to parse command message: ".concat(e.message));
        }
    };
    RTMPLoader.prototype._handleMediaData = function (message) {
        // Convert RTMP message to FLV tag
        var flvTag = Object(_rtmp_protocol_js__WEBPACK_IMPORTED_MODULE_3__["rtmpMessageToFLVTag"])(message);
        if (flvTag) {
            // Convert Node.js Buffer to ArrayBuffer
            var arrayBuffer = flvTag.buffer.slice(flvTag.byteOffset, flvTag.byteOffset + flvTag.byteLength);
            var byteStart = this._receivedLength;
            this._receivedLength += arrayBuffer.byteLength;
            // Deliver FLV tag to FLVDemuxer via callback
            if (this._onDataArrival) {
                this._onDataArrival(arrayBuffer, byteStart, this._receivedLength);
            }
        }
    };
    return RTMPLoader;
}(_loader_js__WEBPACK_IMPORTED_MODULE_1__["BaseLoader"]));
/* harmony default export */ __webpack_exports__["default"] = (RTMPLoader);

/* WEBPACK VAR INJECTION */}.call(this, __webpack_require__(/*! ./../../node_modules/buffer/index.js */ "./node_modules/buffer/index.js").Buffer))

/***/ }),

/***/ "./src/io/rtmp-protocol.js":
/*!*********************************!*\
  !*** ./src/io/rtmp-protocol.js ***!
  \*********************************/
/*! exports provided: RTMPMessageType, RTMPChunkType, RTMPConstants, RTMPChunkHeader, RTMPChunkAggregator, rtmpMessageToFLVTag, parseRTMPURL */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* WEBPACK VAR INJECTION */(function(Buffer) {/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "RTMPMessageType", function() { return RTMPMessageType; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "RTMPChunkType", function() { return RTMPChunkType; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "RTMPConstants", function() { return RTMPConstants; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "RTMPChunkHeader", function() { return RTMPChunkHeader; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "RTMPChunkAggregator", function() { return RTMPChunkAggregator; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "rtmpMessageToFLVTag", function() { return rtmpMessageToFLVTag; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "parseRTMPURL", function() { return parseRTMPURL; });
/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// RTMP Message Types
var RTMPMessageType = {
    SET_CHUNK_SIZE: 1,
    ABORT_MESSAGE: 2,
    ACKNOWLEDGEMENT: 3,
    USER_CONTROL: 4,
    WINDOW_ACK_SIZE: 5,
    SET_PEER_BANDWIDTH: 6,
    AUDIO: 8,
    VIDEO: 9,
    DATA_AMF3: 15,
    SHARED_OBJECT_AMF3: 16,
    COMMAND_AMF3: 17,
    DATA_AMF0: 18,
    SHARED_OBJECT_AMF0: 19,
    COMMAND_AMF0: 20,
    AGGREGATE: 22
};
// RTMP Chunk Format Types
var RTMPChunkType = {
    FULL: 0,
    NO_MSG_STREAM_ID: 1,
    TIMESTAMP_ONLY: 2,
    NO_HEADER: 3 // 0 bytes: continuation chunk
};
// Default RTMP Constants
var RTMPConstants = {
    DEFAULT_CHUNK_SIZE: 128,
    HANDSHAKE_SIZE: 1536,
    PROTOCOL_VERSION: 3,
    DEFAULT_WINDOW_ACK_SIZE: 2500000,
    DEFAULT_PEER_BANDWIDTH: 2500000
};
/**
 * RTMP Chunk Header Parser
 * Parses RTMP basic header and message header
 */
var RTMPChunkHeader = /** @class */ (function () {
    function RTMPChunkHeader() {
        this.fmt = 0; // Chunk format type (0-3)
        this.csid = 0; // Chunk stream ID
        this.timestamp = 0; // Timestamp or timestamp delta
        this.messageLength = 0; // Message length
        this.messageTypeId = 0; // Message type ID
        this.messageStreamId = 0; // Message stream ID
        this.headerLength = 0; // Total header length (basic + message header)
        this.extendedTimestamp = false; // Whether extended timestamp is present
    }
    /**
     * Parse basic header (1-3 bytes)
     * Returns: { csid, fmt, bytesRead }
     */
    RTMPChunkHeader.parseBasicHeader = function (buffer, offset) {
        if (offset >= buffer.length) {
            return null;
        }
        var firstByte = buffer[offset];
        var fmt = (firstByte >> 6) & 0x03;
        var csid = firstByte & 0x3F;
        var bytesRead = 1;
        // CSID encoding:
        // 0: CSID is 64-319 (second byte + 64)
        // 1: CSID is 64-65599 (third byte * 256 + second byte + 64)
        // 2-63: CSID is the value itself
        if (csid === 0) {
            if (offset + 1 >= buffer.length) {
                return null;
            }
            csid = buffer[offset + 1] + 64;
            bytesRead = 2;
        }
        else if (csid === 1) {
            if (offset + 2 >= buffer.length) {
                return null;
            }
            csid = buffer[offset + 2] * 256 + buffer[offset + 1] + 64;
            bytesRead = 3;
        }
        return { csid: csid, fmt: fmt, bytesRead: bytesRead };
    };
    /**
     * Parse message header based on format type
     * Returns: { header: RTMPChunkHeader, bytesRead } or null if insufficient data
     */
    RTMPChunkHeader.parseMessageHeader = function (buffer, offset, basicHeader, prevHeaders) {
        var header = new RTMPChunkHeader();
        header.fmt = basicHeader.fmt;
        header.csid = basicHeader.csid;
        var bytesRead = 0;
        var prevHeader = prevHeaders.get(basicHeader.csid);
        // Determine message header size based on format type
        var messageHeaderSize = 0;
        switch (basicHeader.fmt) {
            case RTMPChunkType.FULL:
                messageHeaderSize = 11;
                break;
            case RTMPChunkType.NO_MSG_STREAM_ID:
                messageHeaderSize = 7;
                break;
            case RTMPChunkType.TIMESTAMP_ONLY:
                messageHeaderSize = 3;
                break;
            case RTMPChunkType.NO_HEADER:
                messageHeaderSize = 0;
                break;
        }
        if (offset + messageHeaderSize > buffer.length) {
            return null; // Not enough data
        }
        // Parse message header fields
        if (basicHeader.fmt === RTMPChunkType.FULL) {
            // Type 0: Full header (11 bytes)
            var timestamp = (buffer[offset] << 16) | (buffer[offset + 1] << 8) | buffer[offset + 2];
            header.messageLength = (buffer[offset + 3] << 16) | (buffer[offset + 4] << 8) | buffer[offset + 5];
            header.messageTypeId = buffer[offset + 6];
            header.messageStreamId = buffer[offset + 7] | (buffer[offset + 8] << 8) |
                (buffer[offset + 9] << 16) | (buffer[offset + 10] << 24);
            header.timestamp = timestamp;
            header.extendedTimestamp = (timestamp === 0xFFFFFF);
            bytesRead = 11;
        }
        else if (basicHeader.fmt === RTMPChunkType.NO_MSG_STREAM_ID) {
            // Type 1: No message stream ID (7 bytes)
            var timestampDelta = (buffer[offset] << 16) | (buffer[offset + 1] << 8) | buffer[offset + 2];
            header.messageLength = (buffer[offset + 3] << 16) | (buffer[offset + 4] << 8) | buffer[offset + 5];
            header.messageTypeId = buffer[offset + 6];
            if (prevHeader) {
                header.messageStreamId = prevHeader.messageStreamId;
                header.timestamp = prevHeader.timestamp + timestampDelta;
            }
            else {
                header.timestamp = timestampDelta;
            }
            header.extendedTimestamp = (timestampDelta === 0xFFFFFF);
            bytesRead = 7;
        }
        else if (basicHeader.fmt === RTMPChunkType.TIMESTAMP_ONLY) {
            // Type 2: Timestamp delta only (3 bytes)
            var timestampDelta = (buffer[offset] << 16) | (buffer[offset + 1] << 8) | buffer[offset + 2];
            if (prevHeader) {
                header.messageLength = prevHeader.messageLength;
                header.messageTypeId = prevHeader.messageTypeId;
                header.messageStreamId = prevHeader.messageStreamId;
                header.timestamp = prevHeader.timestamp + timestampDelta;
            }
            else {
                header.timestamp = timestampDelta;
            }
            header.extendedTimestamp = (timestampDelta === 0xFFFFFF);
            bytesRead = 3;
        }
        else if (basicHeader.fmt === RTMPChunkType.NO_HEADER) {
            // Type 3: No header, reuse everything from previous chunk
            if (prevHeader) {
                header.messageLength = prevHeader.messageLength;
                header.messageTypeId = prevHeader.messageTypeId;
                header.messageStreamId = prevHeader.messageStreamId;
                header.timestamp = prevHeader.timestamp;
                header.extendedTimestamp = prevHeader.extendedTimestamp;
            }
            bytesRead = 0;
        }
        // Handle extended timestamp (4 bytes) if present
        if (header.extendedTimestamp) {
            if (offset + bytesRead + 4 > buffer.length) {
                return null; // Not enough data for extended timestamp
            }
            header.timestamp = (buffer[offset + bytesRead] << 24) |
                (buffer[offset + bytesRead + 1] << 16) |
                (buffer[offset + bytesRead + 2] << 8) |
                buffer[offset + bytesRead + 3];
            bytesRead += 4;
        }
        header.headerLength = bytesRead;
        return { header: header, bytesRead: bytesRead };
    };
    return RTMPChunkHeader;
}());

/**
 * RTMP Chunk Aggregator
 * Aggregates chunks that belong to the same message
 */
var RTMPChunkAggregator = /** @class */ (function () {
    function RTMPChunkAggregator() {
        this.messages = new Map(); // Map<csid, {header, chunks: Buffer[], receivedLength}>
        this.prevHeaders = new Map(); // Map<csid, RTMPChunkHeader>
        this.chunkSize = RTMPConstants.DEFAULT_CHUNK_SIZE;
    }
    RTMPChunkAggregator.prototype.setChunkSize = function (size) {
        this.chunkSize = size;
        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v('RTMPChunkAggregator', "Chunk size set to ".concat(size));
    };
    /**
     * Add chunk data to aggregator
     * Returns complete message buffer when message is fully received, null otherwise
     */
    RTMPChunkAggregator.prototype.addChunk = function (header, chunkData) {
        var csid = header.csid;
        // Store header for future reference
        this.prevHeaders.set(csid, header);
        // Check if this is a new message or continuation
        if (!this.messages.has(csid) || header.fmt === RTMPChunkType.FULL) {
            // New message
            this.messages.set(csid, {
                header: header,
                chunks: [Buffer.from(chunkData)],
                receivedLength: chunkData.length
            });
        }
        else {
            // Continuation of existing message
            var msg_1 = this.messages.get(csid);
            msg_1.chunks.push(Buffer.from(chunkData));
            msg_1.receivedLength += chunkData.length;
        }
        var msg = this.messages.get(csid);
        // Check if message is complete
        if (msg.receivedLength >= msg.header.messageLength) {
            // Concatenate all chunks
            var messageBuffer = Buffer.concat(msg.chunks, msg.header.messageLength);
            // Clear message from aggregator
            this.messages.delete(csid);
            return {
                header: msg.header,
                data: messageBuffer
            };
        }
        return null; // Message not complete yet
    };
    RTMPChunkAggregator.prototype.getPrevHeader = function (csid) {
        return this.prevHeaders.get(csid);
    };
    return RTMPChunkAggregator;
}());

/**
 * Convert RTMP message to FLV tag format
 * FLV tag format:
 *   Tag Type (1 byte): 8=audio, 9=video, 18=script
 *   Data Size (3 bytes): Size of data payload
 *   Timestamp (3 bytes): Lower 24 bits
 *   Timestamp Extended (1 byte): Upper 8 bits
 *   Stream ID (3 bytes): Always 0
 *   Data: Payload
 *   Previous Tag Size (4 bytes): Size of entire tag (11 + data size)
 */
function rtmpMessageToFLVTag(message) {
    var header = message.header, data = message.data;
    var messageType = header.messageTypeId;
    // Only convert audio, video, and script data messages
    if (messageType !== RTMPMessageType.AUDIO &&
        messageType !== RTMPMessageType.VIDEO &&
        messageType !== RTMPMessageType.DATA_AMF0) {
        return null;
    }
    // Map RTMP message type to FLV tag type
    var tagType;
    if (messageType === RTMPMessageType.AUDIO) {
        tagType = 8;
    }
    else if (messageType === RTMPMessageType.VIDEO) {
        tagType = 9;
    }
    else if (messageType === RTMPMessageType.DATA_AMF0) {
        tagType = 18;
    }
    var dataSize = data.length;
    var timestamp = header.timestamp;
    var timestampLower = timestamp & 0xFFFFFF;
    var timestampUpper = (timestamp >> 24) & 0xFF;
    // Allocate buffer for FLV tag (11 bytes header + data + 4 bytes prev tag size)
    var tagBuffer = Buffer.allocUnsafe(11 + dataSize + 4);
    // Write FLV tag header
    tagBuffer[0] = tagType;
    tagBuffer[1] = (dataSize >> 16) & 0xFF;
    tagBuffer[2] = (dataSize >> 8) & 0xFF;
    tagBuffer[3] = dataSize & 0xFF;
    tagBuffer[4] = (timestampLower >> 16) & 0xFF;
    tagBuffer[5] = (timestampLower >> 8) & 0xFF;
    tagBuffer[6] = timestampLower & 0xFF;
    tagBuffer[7] = timestampUpper;
    tagBuffer[8] = 0; // Stream ID (always 0)
    tagBuffer[9] = 0;
    tagBuffer[10] = 0;
    // Copy data payload
    data.copy(tagBuffer, 11);
    // Write previous tag size (11 bytes header + data size)
    var prevTagSize = 11 + dataSize;
    tagBuffer[11 + dataSize] = (prevTagSize >> 24) & 0xFF;
    tagBuffer[11 + dataSize + 1] = (prevTagSize >> 16) & 0xFF;
    tagBuffer[11 + dataSize + 2] = (prevTagSize >> 8) & 0xFF;
    tagBuffer[11 + dataSize + 3] = prevTagSize & 0xFF;
    return tagBuffer;
}
/**
 * Parse RTMP URL
 * Format: rtmp://host[:port]/app/stream[?params]
 */
function parseRTMPURL(url) {
    var match = url.match(/^rtmp:\/\/([^/:]+)(?::(\d+))?(\/[^?]*)?(?:\?(.*))?$/);
    if (!match) {
        throw new Error("Invalid RTMP URL: ".concat(url));
    }
    var host = match[1];
    var port = match[2] ? parseInt(match[2], 10) : 1935;
    var path = match[3] || '/';
    var query = match[4] || '';
    // Split path into app and stream
    // Format: /app/stream or /app/instance/stream
    var pathParts = path.split('/').filter(function (p) { return p.length > 0; });
    if (pathParts.length < 2) {
        throw new Error("Invalid RTMP path: ".concat(path, ". Expected format: /app/stream"));
    }
    // App is everything except the last part (which is the stream name)
    var stream = pathParts[pathParts.length - 1];
    var app = pathParts.slice(0, -1).join('/');
    return {
        host: host,
        port: port,
        app: app,
        stream: stream,
        tcUrl: "rtmp://".concat(host, ":").concat(port, "/").concat(app),
        query: query
    };
}

/* WEBPACK VAR INJECTION */}.call(this, __webpack_require__(/*! ./../../node_modules/buffer/index.js */ "./node_modules/buffer/index.js").Buffer))

/***/ }),

/***/ "./src/io/speed-sampler.js":
/*!*********************************!*\
  !*** ./src/io/speed-sampler.js ***!
  \*********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
// Utility class to calculate realtime network I/O speed
var SpeedSampler = /** @class */ (function () {
    function SpeedSampler() {
        // milliseconds
        this._firstCheckpoint = 0;
        this._lastCheckpoint = 0;
        this._intervalBytes = 0;
        this._totalBytes = 0;
        this._lastSecondBytes = 0;
        // compatibility detection
        if (self.performance && self.performance.now) {
            this._now = self.performance.now.bind(self.performance);
        }
        else {
            this._now = Date.now;
        }
    }
    SpeedSampler.prototype.reset = function () {
        this._firstCheckpoint = this._lastCheckpoint = 0;
        this._totalBytes = this._intervalBytes = 0;
        this._lastSecondBytes = 0;
    };
    SpeedSampler.prototype.addBytes = function (bytes) {
        if (this._firstCheckpoint === 0) {
            this._firstCheckpoint = this._now();
            this._lastCheckpoint = this._firstCheckpoint;
            this._intervalBytes += bytes;
            this._totalBytes += bytes;
        }
        else if (this._now() - this._lastCheckpoint < 1000) {
            this._intervalBytes += bytes;
            this._totalBytes += bytes;
        }
        else { // duration >= 1000
            this._lastSecondBytes = this._intervalBytes;
            this._intervalBytes = bytes;
            this._totalBytes += bytes;
            this._lastCheckpoint = this._now();
        }
    };
    Object.defineProperty(SpeedSampler.prototype, "currentKBps", {
        get: function () {
            this.addBytes(0);
            var durationSeconds = (this._now() - this._lastCheckpoint) / 1000;
            if (durationSeconds == 0)
                durationSeconds = 1;
            return (this._intervalBytes / durationSeconds) / 1024;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(SpeedSampler.prototype, "lastSecondKBps", {
        get: function () {
            this.addBytes(0);
            if (this._lastSecondBytes !== 0) {
                return this._lastSecondBytes / 1024;
            }
            else { // lastSecondBytes === 0
                if (this._now() - this._lastCheckpoint >= 500) {
                    // if time interval since last checkpoint has exceeded 500ms
                    // the speed is nearly accurate
                    return this.currentKBps;
                }
                else {
                    // We don't know
                    return 0;
                }
            }
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(SpeedSampler.prototype, "averageKBps", {
        get: function () {
            var durationSeconds = (this._now() - this._firstCheckpoint) / 1000;
            return (this._totalBytes / durationSeconds) / 1024;
        },
        enumerable: false,
        configurable: true
    });
    return SpeedSampler;
}());
/* harmony default export */ __webpack_exports__["default"] = (SpeedSampler);


/***/ }),

/***/ "./src/io/websocket-loader.js":
/*!************************************!*\
  !*** ./src/io/websocket-loader.js ***!
  \************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/* harmony import */ var _loader_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ./loader.js */ "./src/io/loader.js");
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ../utils/exception.js */ "./src/utils/exception.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var __extends = (undefined && undefined.__extends) || (function () {
    var extendStatics = function (d, b) {
        extendStatics = Object.setPrototypeOf ||
            ({ __proto__: [] } instanceof Array && function (d, b) { d.__proto__ = b; }) ||
            function (d, b) { for (var p in b) if (Object.prototype.hasOwnProperty.call(b, p)) d[p] = b[p]; };
        return extendStatics(d, b);
    };
    return function (d, b) {
        if (typeof b !== "function" && b !== null)
            throw new TypeError("Class extends value " + String(b) + " is not a constructor or null");
        extendStatics(d, b);
        function __() { this.constructor = d; }
        d.prototype = b === null ? Object.create(b) : (__.prototype = b.prototype, new __());
    };
})();



// For MPEG-TS/FLV over WebSocket live stream
var WebSocketLoader = /** @class */ (function (_super) {
    __extends(WebSocketLoader, _super);
    function WebSocketLoader() {
        var _this = _super.call(this, 'websocket-loader') || this;
        _this.TAG = 'WebSocketLoader';
        _this._needStash = true;
        _this._ws = null;
        _this._requestAbort = false;
        _this._receivedLength = 0;
        return _this;
    }
    WebSocketLoader.isSupported = function () {
        try {
            return (typeof self.WebSocket !== 'undefined');
        }
        catch (e) {
            return false;
        }
    };
    WebSocketLoader.prototype.destroy = function () {
        if (this._ws) {
            this.abort();
        }
        _super.prototype.destroy.call(this);
    };
    WebSocketLoader.prototype.open = function (dataSource) {
        try {
            var ws = this._ws = new self.WebSocket(dataSource.url);
            ws.binaryType = 'arraybuffer';
            ws.onopen = this._onWebSocketOpen.bind(this);
            ws.onclose = this._onWebSocketClose.bind(this);
            ws.onmessage = this._onWebSocketMessage.bind(this);
            ws.onerror = this._onWebSocketError.bind(this);
            this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kConnecting;
        }
        catch (e) {
            this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kError;
            var info = { code: e.code, msg: e.message };
            if (this._onError) {
                this._onError(_loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderErrors"].EXCEPTION, info);
            }
            else {
                throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["RuntimeException"](info.msg);
            }
        }
    };
    WebSocketLoader.prototype.abort = function () {
        var ws = this._ws;
        if (ws && (ws.readyState === 0 || ws.readyState === 1)) { // CONNECTING || OPEN
            this._requestAbort = true;
            ws.close();
        }
        this._ws = null;
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kComplete;
    };
    WebSocketLoader.prototype._onWebSocketOpen = function (e) {
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kBuffering;
    };
    WebSocketLoader.prototype._onWebSocketClose = function (e) {
        if (this._requestAbort === true) {
            this._requestAbort = false;
            return;
        }
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kComplete;
        if (this._onComplete) {
            this._onComplete(0, this._receivedLength - 1);
        }
    };
    WebSocketLoader.prototype._onWebSocketMessage = function (e) {
        var _this = this;
        if (e.data instanceof ArrayBuffer) {
            this._dispatchArrayBuffer(e.data);
        }
        else if (e.data instanceof Blob) {
            var reader_1 = new FileReader();
            reader_1.onload = function () {
                _this._dispatchArrayBuffer(reader_1.result);
            };
            reader_1.readAsArrayBuffer(e.data);
        }
        else {
            this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kError;
            var info = { code: -1, msg: 'Unsupported WebSocket message type: ' + e.data.constructor.name };
            if (this._onError) {
                this._onError(_loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderErrors"].EXCEPTION, info);
            }
            else {
                throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["RuntimeException"](info.msg);
            }
        }
    };
    WebSocketLoader.prototype._dispatchArrayBuffer = function (arraybuffer) {
        var chunk = arraybuffer;
        var byteStart = this._receivedLength;
        this._receivedLength += chunk.byteLength;
        if (this._onDataArrival) {
            this._onDataArrival(chunk, byteStart, this._receivedLength);
        }
    };
    WebSocketLoader.prototype._onWebSocketError = function (e) {
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kError;
        var info = {
            code: e.code,
            msg: e.message
        };
        if (this._onError) {
            this._onError(_loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderErrors"].EXCEPTION, info);
        }
        else {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["RuntimeException"](info.msg);
        }
    };
    return WebSocketLoader;
}(_loader_js__WEBPACK_IMPORTED_MODULE_1__["BaseLoader"]));
/* harmony default export */ __webpack_exports__["default"] = (WebSocketLoader);


/***/ }),

/***/ "./src/io/xhr-moz-chunked-loader.js":
/*!******************************************!*\
  !*** ./src/io/xhr-moz-chunked-loader.js ***!
  \******************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/* harmony import */ var _loader_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ./loader.js */ "./src/io/loader.js");
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ../utils/exception.js */ "./src/utils/exception.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var __extends = (undefined && undefined.__extends) || (function () {
    var extendStatics = function (d, b) {
        extendStatics = Object.setPrototypeOf ||
            ({ __proto__: [] } instanceof Array && function (d, b) { d.__proto__ = b; }) ||
            function (d, b) { for (var p in b) if (Object.prototype.hasOwnProperty.call(b, p)) d[p] = b[p]; };
        return extendStatics(d, b);
    };
    return function (d, b) {
        if (typeof b !== "function" && b !== null)
            throw new TypeError("Class extends value " + String(b) + " is not a constructor or null");
        extendStatics(d, b);
        function __() { this.constructor = d; }
        d.prototype = b === null ? Object.create(b) : (__.prototype = b.prototype, new __());
    };
})();



// For FireFox browser which supports `xhr.responseType = 'moz-chunked-arraybuffer'`
var MozChunkedLoader = /** @class */ (function (_super) {
    __extends(MozChunkedLoader, _super);
    function MozChunkedLoader(seekHandler, config) {
        var _this = _super.call(this, 'xhr-moz-chunked-loader') || this;
        _this.TAG = 'MozChunkedLoader';
        _this._seekHandler = seekHandler;
        _this._config = config;
        _this._needStash = true;
        _this._xhr = null;
        _this._requestAbort = false;
        _this._contentLength = null;
        _this._receivedLength = 0;
        return _this;
    }
    MozChunkedLoader.isSupported = function () {
        try {
            var xhr = new XMLHttpRequest();
            // Firefox 37- requires .open() to be called before setting responseType
            xhr.open('GET', 'https://example.com', true);
            xhr.responseType = 'moz-chunked-arraybuffer';
            return (xhr.responseType === 'moz-chunked-arraybuffer');
        }
        catch (e) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w('MozChunkedLoader', e.message);
            return false;
        }
    };
    MozChunkedLoader.prototype.destroy = function () {
        if (this.isWorking()) {
            this.abort();
        }
        if (this._xhr) {
            this._xhr.onreadystatechange = null;
            this._xhr.onprogress = null;
            this._xhr.onloadend = null;
            this._xhr.onerror = null;
            this._xhr = null;
        }
        _super.prototype.destroy.call(this);
    };
    MozChunkedLoader.prototype.open = function (dataSource, range) {
        this._dataSource = dataSource;
        this._range = range;
        var sourceURL = dataSource.url;
        if (this._config.reuseRedirectedURL && dataSource.redirectedURL != undefined) {
            sourceURL = dataSource.redirectedURL;
        }
        var seekConfig = this._seekHandler.getConfig(sourceURL, range);
        this._requestURL = seekConfig.url;
        var xhr = this._xhr = new XMLHttpRequest();
        xhr.open('GET', seekConfig.url, true);
        xhr.responseType = 'moz-chunked-arraybuffer';
        xhr.onreadystatechange = this._onReadyStateChange.bind(this);
        xhr.onprogress = this._onProgress.bind(this);
        xhr.onloadend = this._onLoadEnd.bind(this);
        xhr.onerror = this._onXhrError.bind(this);
        // cors is auto detected and enabled by xhr
        // withCredentials is disabled by default
        if (dataSource.withCredentials) {
            xhr.withCredentials = true;
        }
        if (typeof seekConfig.headers === 'object') {
            var headers = seekConfig.headers;
            for (var key in headers) {
                if (headers.hasOwnProperty(key)) {
                    xhr.setRequestHeader(key, headers[key]);
                }
            }
        }
        // add additional headers
        if (typeof this._config.headers === 'object') {
            var headers = this._config.headers;
            for (var key in headers) {
                if (headers.hasOwnProperty(key)) {
                    xhr.setRequestHeader(key, headers[key]);
                }
            }
        }
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kConnecting;
        xhr.send();
    };
    MozChunkedLoader.prototype.abort = function () {
        this._requestAbort = true;
        if (this._xhr) {
            this._xhr.abort();
        }
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kComplete;
    };
    MozChunkedLoader.prototype._onReadyStateChange = function (e) {
        var xhr = e.target;
        if (xhr.readyState === 2) { // HEADERS_RECEIVED
            if (xhr.responseURL != undefined && xhr.responseURL !== this._requestURL) {
                if (this._onURLRedirect) {
                    var redirectedURL = this._seekHandler.removeURLParameters(xhr.responseURL);
                    this._onURLRedirect(redirectedURL);
                }
            }
            if (xhr.status !== 0 && (xhr.status < 200 || xhr.status > 299)) {
                this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kError;
                if (this._onError) {
                    this._onError(_loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderErrors"].HTTP_STATUS_CODE_INVALID, { code: xhr.status, msg: xhr.statusText });
                }
                else {
                    throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["RuntimeException"]('MozChunkedLoader: Http code invalid, ' + xhr.status + ' ' + xhr.statusText);
                }
            }
            else {
                this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kBuffering;
            }
        }
    };
    MozChunkedLoader.prototype._onProgress = function (e) {
        if (this._status === _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kError) {
            // Ignore error response
            return;
        }
        if (this._contentLength === null) {
            if (e.total !== null && e.total !== 0) {
                this._contentLength = e.total;
                if (this._onContentLengthKnown) {
                    this._onContentLengthKnown(this._contentLength);
                }
            }
        }
        var chunk = e.target.response;
        var byteStart = this._range.from + this._receivedLength;
        this._receivedLength += chunk.byteLength;
        if (this._onDataArrival) {
            this._onDataArrival(chunk, byteStart, this._receivedLength);
        }
    };
    MozChunkedLoader.prototype._onLoadEnd = function (e) {
        if (this._requestAbort === true) {
            this._requestAbort = false;
            return;
        }
        else if (this._status === _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kError) {
            return;
        }
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kComplete;
        if (this._onComplete) {
            this._onComplete(this._range.from, this._range.from + this._receivedLength - 1);
        }
    };
    MozChunkedLoader.prototype._onXhrError = function (e) {
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kError;
        var type = 0;
        var info = null;
        if (this._contentLength && e.loaded < this._contentLength) {
            type = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderErrors"].EARLY_EOF;
            info = { code: -1, msg: 'Moz-Chunked stream meet Early-Eof' };
        }
        else {
            type = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderErrors"].EXCEPTION;
            info = { code: -1, msg: e.constructor.name + ' ' + e.type };
        }
        if (this._onError) {
            this._onError(type, info);
        }
        else {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["RuntimeException"](info.msg);
        }
    };
    return MozChunkedLoader;
}(_loader_js__WEBPACK_IMPORTED_MODULE_1__["BaseLoader"]));
/* harmony default export */ __webpack_exports__["default"] = (MozChunkedLoader);


/***/ }),

/***/ "./src/io/xhr-msstream-loader.js":
/*!***************************************!*\
  !*** ./src/io/xhr-msstream-loader.js ***!
  \***************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/* harmony import */ var _loader_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ./loader.js */ "./src/io/loader.js");
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ../utils/exception.js */ "./src/utils/exception.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var __extends = (undefined && undefined.__extends) || (function () {
    var extendStatics = function (d, b) {
        extendStatics = Object.setPrototypeOf ||
            ({ __proto__: [] } instanceof Array && function (d, b) { d.__proto__ = b; }) ||
            function (d, b) { for (var p in b) if (Object.prototype.hasOwnProperty.call(b, p)) d[p] = b[p]; };
        return extendStatics(d, b);
    };
    return function (d, b) {
        if (typeof b !== "function" && b !== null)
            throw new TypeError("Class extends value " + String(b) + " is not a constructor or null");
        extendStatics(d, b);
        function __() { this.constructor = d; }
        d.prototype = b === null ? Object.create(b) : (__.prototype = b.prototype, new __());
    };
})();



/* Notice: ms-stream may cause IE/Edge browser crash if seek too frequently!!!
 * The browser may crash in wininet.dll. Disable for now.
 *
 * For IE11/Edge browser by microsoft which supports `xhr.responseType = 'ms-stream'`
 * Notice that ms-stream API sucks. The buffer is always expanding along with downloading.
 *
 * We need to abort the xhr if buffer size exceeded limit size (e.g. 16 MiB), then do reconnect.
 * in order to release previous ArrayBuffer to avoid memory leak
 *
 * Otherwise, the ArrayBuffer will increase to a terrible size that equals final file size.
 */
var MSStreamLoader = /** @class */ (function (_super) {
    __extends(MSStreamLoader, _super);
    function MSStreamLoader(seekHandler, config) {
        var _this = _super.call(this, 'xhr-msstream-loader') || this;
        _this.TAG = 'MSStreamLoader';
        _this._seekHandler = seekHandler;
        _this._config = config;
        _this._needStash = true;
        _this._xhr = null;
        _this._reader = null; // MSStreamReader
        _this._totalRange = null;
        _this._currentRange = null;
        _this._currentRequestURL = null;
        _this._currentRedirectedURL = null;
        _this._contentLength = null;
        _this._receivedLength = 0;
        _this._bufferLimit = 16 * 1024 * 1024; // 16MB
        _this._lastTimeBufferSize = 0;
        _this._isReconnecting = false;
        return _this;
    }
    MSStreamLoader.isSupported = function () {
        try {
            if (typeof self.MSStream === 'undefined' || typeof self.MSStreamReader === 'undefined') {
                return false;
            }
            var xhr = new XMLHttpRequest();
            xhr.open('GET', 'https://example.com', true);
            xhr.responseType = 'ms-stream';
            return (xhr.responseType === 'ms-stream');
        }
        catch (e) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w('MSStreamLoader', e.message);
            return false;
        }
    };
    MSStreamLoader.prototype.destroy = function () {
        if (this.isWorking()) {
            this.abort();
        }
        if (this._reader) {
            this._reader.onprogress = null;
            this._reader.onload = null;
            this._reader.onerror = null;
            this._reader = null;
        }
        if (this._xhr) {
            this._xhr.onreadystatechange = null;
            this._xhr = null;
        }
        _super.prototype.destroy.call(this);
    };
    MSStreamLoader.prototype.open = function (dataSource, range) {
        this._internalOpen(dataSource, range, false);
    };
    MSStreamLoader.prototype._internalOpen = function (dataSource, range, isSubrange) {
        this._dataSource = dataSource;
        if (!isSubrange) {
            this._totalRange = range;
        }
        else {
            this._currentRange = range;
        }
        var sourceURL = dataSource.url;
        if (this._config.reuseRedirectedURL) {
            if (this._currentRedirectedURL != undefined) {
                sourceURL = this._currentRedirectedURL;
            }
            else if (dataSource.redirectedURL != undefined) {
                sourceURL = dataSource.redirectedURL;
            }
        }
        var seekConfig = this._seekHandler.getConfig(sourceURL, range);
        this._currentRequestURL = seekConfig.url;
        var reader = this._reader = new self.MSStreamReader();
        reader.onprogress = this._msrOnProgress.bind(this);
        reader.onload = this._msrOnLoad.bind(this);
        reader.onerror = this._msrOnError.bind(this);
        var xhr = this._xhr = new XMLHttpRequest();
        xhr.open('GET', seekConfig.url, true);
        xhr.responseType = 'ms-stream';
        xhr.onreadystatechange = this._xhrOnReadyStateChange.bind(this);
        xhr.onerror = this._xhrOnError.bind(this);
        if (dataSource.withCredentials) {
            xhr.withCredentials = true;
        }
        if (typeof seekConfig.headers === 'object') {
            var headers = seekConfig.headers;
            for (var key in headers) {
                if (headers.hasOwnProperty(key)) {
                    xhr.setRequestHeader(key, headers[key]);
                }
            }
        }
        // add additional headers
        if (typeof this._config.headers === 'object') {
            var headers = this._config.headers;
            for (var key in headers) {
                if (headers.hasOwnProperty(key)) {
                    xhr.setRequestHeader(key, headers[key]);
                }
            }
        }
        if (this._isReconnecting) {
            this._isReconnecting = false;
        }
        else {
            this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kConnecting;
        }
        xhr.send();
    };
    MSStreamLoader.prototype.abort = function () {
        this._internalAbort();
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kComplete;
    };
    MSStreamLoader.prototype._internalAbort = function () {
        if (this._reader) {
            if (this._reader.readyState === 1) { // LOADING
                this._reader.abort();
            }
            this._reader.onprogress = null;
            this._reader.onload = null;
            this._reader.onerror = null;
            this._reader = null;
        }
        if (this._xhr) {
            this._xhr.abort();
            this._xhr.onreadystatechange = null;
            this._xhr = null;
        }
    };
    MSStreamLoader.prototype._xhrOnReadyStateChange = function (e) {
        var xhr = e.target;
        if (xhr.readyState === 2) { // HEADERS_RECEIVED
            if (xhr.status >= 200 && xhr.status <= 299) {
                this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kBuffering;
                if (xhr.responseURL != undefined) {
                    var redirectedURL = this._seekHandler.removeURLParameters(xhr.responseURL);
                    if (xhr.responseURL !== this._currentRequestURL && redirectedURL !== this._currentRedirectedURL) {
                        this._currentRedirectedURL = redirectedURL;
                        if (this._onURLRedirect) {
                            this._onURLRedirect(redirectedURL);
                        }
                    }
                }
                var lengthHeader = xhr.getResponseHeader('Content-Length');
                if (lengthHeader != null && this._contentLength == null) {
                    var length_1 = parseInt(lengthHeader);
                    if (length_1 > 0) {
                        this._contentLength = length_1;
                        if (this._onContentLengthKnown) {
                            this._onContentLengthKnown(this._contentLength);
                        }
                    }
                }
            }
            else {
                this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kError;
                if (this._onError) {
                    this._onError(_loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderErrors"].HTTP_STATUS_CODE_INVALID, { code: xhr.status, msg: xhr.statusText });
                }
                else {
                    throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["RuntimeException"]('MSStreamLoader: Http code invalid, ' + xhr.status + ' ' + xhr.statusText);
                }
            }
        }
        else if (xhr.readyState === 3) { // LOADING
            if (xhr.status >= 200 && xhr.status <= 299) {
                this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kBuffering;
                var msstream = xhr.response;
                this._reader.readAsArrayBuffer(msstream);
            }
        }
    };
    MSStreamLoader.prototype._xhrOnError = function (e) {
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kError;
        var type = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderErrors"].EXCEPTION;
        var info = { code: -1, msg: e.constructor.name + ' ' + e.type };
        if (this._onError) {
            this._onError(type, info);
        }
        else {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["RuntimeException"](info.msg);
        }
    };
    MSStreamLoader.prototype._msrOnProgress = function (e) {
        var reader = e.target;
        var bigbuffer = reader.result;
        if (bigbuffer == null) { // result may be null, workaround for buggy M$
            this._doReconnectIfNeeded();
            return;
        }
        var slice = bigbuffer.slice(this._lastTimeBufferSize);
        this._lastTimeBufferSize = bigbuffer.byteLength;
        var byteStart = this._totalRange.from + this._receivedLength;
        this._receivedLength += slice.byteLength;
        if (this._onDataArrival) {
            this._onDataArrival(slice, byteStart, this._receivedLength);
        }
        if (bigbuffer.byteLength >= this._bufferLimit) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "MSStream buffer exceeded max size near ".concat(byteStart + slice.byteLength, ", reconnecting..."));
            this._doReconnectIfNeeded();
        }
    };
    MSStreamLoader.prototype._doReconnectIfNeeded = function () {
        if (this._contentLength == null || this._receivedLength < this._contentLength) {
            this._isReconnecting = true;
            this._lastTimeBufferSize = 0;
            this._internalAbort();
            var range = {
                from: this._totalRange.from + this._receivedLength,
                to: -1
            };
            this._internalOpen(this._dataSource, range, true);
        }
    };
    MSStreamLoader.prototype._msrOnLoad = function (e) {
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kComplete;
        if (this._onComplete) {
            this._onComplete(this._totalRange.from, this._totalRange.from + this._receivedLength - 1);
        }
    };
    MSStreamLoader.prototype._msrOnError = function (e) {
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderStatus"].kError;
        var type = 0;
        var info = null;
        if (this._contentLength && this._receivedLength < this._contentLength) {
            type = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderErrors"].EARLY_EOF;
            info = { code: -1, msg: 'MSStream meet Early-Eof' };
        }
        else {
            type = _loader_js__WEBPACK_IMPORTED_MODULE_1__["LoaderErrors"].EARLY_EOF;
            info = { code: -1, msg: e.constructor.name + ' ' + e.type };
        }
        if (this._onError) {
            this._onError(type, info);
        }
        else {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_2__["RuntimeException"](info.msg);
        }
    };
    return MSStreamLoader;
}(_loader_js__WEBPACK_IMPORTED_MODULE_1__["BaseLoader"]));
/* harmony default export */ __webpack_exports__["default"] = (MSStreamLoader);


/***/ }),

/***/ "./src/io/xhr-range-loader.js":
/*!************************************!*\
  !*** ./src/io/xhr-range-loader.js ***!
  \************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/* harmony import */ var _speed_sampler_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ./speed-sampler.js */ "./src/io/speed-sampler.js");
/* harmony import */ var _loader_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ./loader.js */ "./src/io/loader.js");
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ../utils/exception.js */ "./src/utils/exception.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var __extends = (undefined && undefined.__extends) || (function () {
    var extendStatics = function (d, b) {
        extendStatics = Object.setPrototypeOf ||
            ({ __proto__: [] } instanceof Array && function (d, b) { d.__proto__ = b; }) ||
            function (d, b) { for (var p in b) if (Object.prototype.hasOwnProperty.call(b, p)) d[p] = b[p]; };
        return extendStatics(d, b);
    };
    return function (d, b) {
        if (typeof b !== "function" && b !== null)
            throw new TypeError("Class extends value " + String(b) + " is not a constructor or null");
        extendStatics(d, b);
        function __() { this.constructor = d; }
        d.prototype = b === null ? Object.create(b) : (__.prototype = b.prototype, new __());
    };
})();




// Universal IO Loader, implemented by adding Range header in xhr's request header
var RangeLoader = /** @class */ (function (_super) {
    __extends(RangeLoader, _super);
    function RangeLoader(seekHandler, config) {
        var _this = _super.call(this, 'xhr-range-loader') || this;
        _this.TAG = 'RangeLoader';
        _this._seekHandler = seekHandler;
        _this._config = config;
        _this._needStash = false;
        _this._chunkSizeKBList = [
            128, 256, 384, 512, 768, 1024, 1536, 2048, 3072, 4096, 5120, 6144, 7168, 8192
        ];
        _this._currentChunkSizeKB = 384;
        _this._currentSpeedNormalized = 0;
        _this._zeroSpeedChunkCount = 0;
        _this._xhr = null;
        _this._speedSampler = new _speed_sampler_js__WEBPACK_IMPORTED_MODULE_1__["default"]();
        _this._requestAbort = false;
        _this._waitForTotalLength = false;
        _this._totalLengthReceived = false;
        _this._currentRequestURL = null;
        _this._currentRedirectedURL = null;
        _this._currentRequestRange = null;
        _this._totalLength = null; // size of the entire file
        _this._contentLength = null; // Content-Length of entire request range
        _this._receivedLength = 0; // total received bytes
        _this._lastTimeLoaded = 0; // received bytes of current request sub-range
        return _this;
    }
    RangeLoader.isSupported = function () {
        try {
            var xhr = new XMLHttpRequest();
            xhr.open('GET', 'https://example.com', true);
            xhr.responseType = 'arraybuffer';
            return (xhr.responseType === 'arraybuffer');
        }
        catch (e) {
            _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w('RangeLoader', e.message);
            return false;
        }
    };
    RangeLoader.prototype.destroy = function () {
        if (this.isWorking()) {
            this.abort();
        }
        if (this._xhr) {
            this._xhr.onreadystatechange = null;
            this._xhr.onprogress = null;
            this._xhr.onload = null;
            this._xhr.onerror = null;
            this._xhr = null;
        }
        _super.prototype.destroy.call(this);
    };
    Object.defineProperty(RangeLoader.prototype, "currentSpeed", {
        get: function () {
            return this._speedSampler.lastSecondKBps;
        },
        enumerable: false,
        configurable: true
    });
    RangeLoader.prototype.open = function (dataSource, range) {
        this._dataSource = dataSource;
        this._range = range;
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kConnecting;
        var useRefTotalLength = false;
        if (this._dataSource.filesize != undefined && this._dataSource.filesize !== 0) {
            useRefTotalLength = true;
            this._totalLength = this._dataSource.filesize;
        }
        if (!this._totalLengthReceived && !useRefTotalLength) {
            // We need total filesize
            this._waitForTotalLength = true;
            this._internalOpen(this._dataSource, { from: 0, to: -1 });
        }
        else {
            // We have filesize, start loading
            this._openSubRange();
        }
    };
    RangeLoader.prototype._openSubRange = function () {
        var chunkSize = this._currentChunkSizeKB * 1024;
        var from = this._range.from + this._receivedLength;
        var to = from + chunkSize;
        if (this._contentLength != null) {
            if (to - this._range.from >= this._contentLength) {
                to = this._range.from + this._contentLength - 1;
            }
        }
        this._currentRequestRange = { from: from, to: to };
        this._internalOpen(this._dataSource, this._currentRequestRange);
    };
    RangeLoader.prototype._internalOpen = function (dataSource, range) {
        this._lastTimeLoaded = 0;
        var sourceURL = dataSource.url;
        if (this._config.reuseRedirectedURL) {
            if (this._currentRedirectedURL != undefined) {
                sourceURL = this._currentRedirectedURL;
            }
            else if (dataSource.redirectedURL != undefined) {
                sourceURL = dataSource.redirectedURL;
            }
        }
        var seekConfig = this._seekHandler.getConfig(sourceURL, range);
        this._currentRequestURL = seekConfig.url;
        var xhr = this._xhr = new XMLHttpRequest();
        xhr.open('GET', seekConfig.url, true);
        xhr.responseType = 'arraybuffer';
        xhr.onreadystatechange = this._onReadyStateChange.bind(this);
        xhr.onprogress = this._onProgress.bind(this);
        xhr.onload = this._onLoad.bind(this);
        xhr.onerror = this._onXhrError.bind(this);
        if (dataSource.withCredentials) {
            xhr.withCredentials = true;
        }
        if (typeof seekConfig.headers === 'object') {
            var headers = seekConfig.headers;
            for (var key in headers) {
                if (headers.hasOwnProperty(key)) {
                    xhr.setRequestHeader(key, headers[key]);
                }
            }
        }
        // add additional headers
        if (typeof this._config.headers === 'object') {
            var headers = this._config.headers;
            for (var key in headers) {
                if (headers.hasOwnProperty(key)) {
                    xhr.setRequestHeader(key, headers[key]);
                }
            }
        }
        xhr.send();
    };
    RangeLoader.prototype.abort = function () {
        this._requestAbort = true;
        this._internalAbort();
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kComplete;
    };
    RangeLoader.prototype._internalAbort = function () {
        if (this._xhr) {
            this._xhr.onreadystatechange = null;
            this._xhr.onprogress = null;
            this._xhr.onload = null;
            this._xhr.onerror = null;
            this._xhr.abort();
            this._xhr = null;
        }
    };
    RangeLoader.prototype._onReadyStateChange = function (e) {
        var xhr = e.target;
        if (xhr.readyState === 2) { // HEADERS_RECEIVED
            if (xhr.responseURL != undefined) { // if the browser support this property
                var redirectedURL = this._seekHandler.removeURLParameters(xhr.responseURL);
                if (xhr.responseURL !== this._currentRequestURL && redirectedURL !== this._currentRedirectedURL) {
                    this._currentRedirectedURL = redirectedURL;
                    if (this._onURLRedirect) {
                        this._onURLRedirect(redirectedURL);
                    }
                }
            }
            if ((xhr.status >= 200 && xhr.status <= 299)) {
                if (this._waitForTotalLength) {
                    return;
                }
                this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kBuffering;
            }
            else {
                this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kError;
                if (this._onError) {
                    this._onError(_loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].HTTP_STATUS_CODE_INVALID, { code: xhr.status, msg: xhr.statusText });
                }
                else {
                    throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_3__["RuntimeException"]('RangeLoader: Http code invalid, ' + xhr.status + ' ' + xhr.statusText);
                }
            }
        }
    };
    RangeLoader.prototype._onProgress = function (e) {
        if (this._status === _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kError) {
            // Ignore error response
            return;
        }
        if (this._contentLength === null) {
            var openNextRange = false;
            if (this._waitForTotalLength) {
                this._waitForTotalLength = false;
                this._totalLengthReceived = true;
                openNextRange = true;
                var total = e.total;
                this._internalAbort();
                if (total != null & total !== 0) {
                    this._totalLength = total;
                }
            }
            // calculate currrent request range's contentLength
            if (this._range.to === -1) {
                this._contentLength = this._totalLength - this._range.from;
            }
            else { // to !== -1
                this._contentLength = this._range.to - this._range.from + 1;
            }
            if (openNextRange) {
                this._openSubRange();
                return;
            }
            if (this._onContentLengthKnown) {
                this._onContentLengthKnown(this._contentLength);
            }
        }
        var delta = e.loaded - this._lastTimeLoaded;
        this._lastTimeLoaded = e.loaded;
        this._speedSampler.addBytes(delta);
    };
    RangeLoader.prototype._normalizeSpeed = function (input) {
        var list = this._chunkSizeKBList;
        var last = list.length - 1;
        var mid = 0;
        var lbound = 0;
        var ubound = last;
        if (input < list[0]) {
            return list[0];
        }
        while (lbound <= ubound) {
            mid = lbound + Math.floor((ubound - lbound) / 2);
            if (mid === last || (input >= list[mid] && input < list[mid + 1])) {
                return list[mid];
            }
            else if (list[mid] < input) {
                lbound = mid + 1;
            }
            else {
                ubound = mid - 1;
            }
        }
    };
    RangeLoader.prototype._onLoad = function (e) {
        if (this._status === _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kError) {
            // Ignore error response
            return;
        }
        if (this._waitForTotalLength) {
            this._waitForTotalLength = false;
            return;
        }
        this._lastTimeLoaded = 0;
        var KBps = this._speedSampler.lastSecondKBps;
        if (KBps === 0) {
            this._zeroSpeedChunkCount++;
            if (this._zeroSpeedChunkCount >= 3) {
                // Try get currentKBps after 3 chunks
                KBps = this._speedSampler.currentKBps;
            }
        }
        if (KBps !== 0) {
            var normalized = this._normalizeSpeed(KBps);
            if (this._currentSpeedNormalized !== normalized) {
                this._currentSpeedNormalized = normalized;
                this._currentChunkSizeKB = normalized;
            }
        }
        var chunk = e.target.response;
        var byteStart = this._range.from + this._receivedLength;
        this._receivedLength += chunk.byteLength;
        var reportComplete = false;
        if (this._contentLength != null && this._receivedLength < this._contentLength) {
            // continue load next chunk
            this._openSubRange();
        }
        else {
            reportComplete = true;
        }
        // dispatch received chunk
        if (this._onDataArrival) {
            this._onDataArrival(chunk, byteStart, this._receivedLength);
        }
        if (reportComplete) {
            this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kComplete;
            if (this._onComplete) {
                this._onComplete(this._range.from, this._range.from + this._receivedLength - 1);
            }
        }
    };
    RangeLoader.prototype._onXhrError = function (e) {
        this._status = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"].kError;
        var type = 0;
        var info = null;
        if (this._contentLength && this._receivedLength > 0
            && this._receivedLength < this._contentLength) {
            type = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].EARLY_EOF;
            info = { code: -1, msg: 'RangeLoader meet Early-Eof' };
        }
        else {
            type = _loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"].EXCEPTION;
            info = { code: -1, msg: e.constructor.name + ' ' + e.type };
        }
        if (this._onError) {
            this._onError(type, info);
        }
        else {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_3__["RuntimeException"](info.msg);
        }
    };
    return RangeLoader;
}(_loader_js__WEBPACK_IMPORTED_MODULE_2__["BaseLoader"]));
/* harmony default export */ __webpack_exports__["default"] = (RangeLoader);


/***/ }),

/***/ "./src/mpegts.js":
/*!***********************!*\
  !*** ./src/mpegts.js ***!
  \***********************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_polyfill_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ./utils/polyfill.js */ "./src/utils/polyfill.js");
/* harmony import */ var _core_features_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ./core/features.js */ "./src/core/features.js");
/* harmony import */ var _io_loader_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ./io/loader.js */ "./src/io/loader.js");
/* harmony import */ var _player_mse_player__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ./player/mse-player */ "./src/player/mse-player.ts");
/* harmony import */ var _player_native_player_js__WEBPACK_IMPORTED_MODULE_4__ = __webpack_require__(/*! ./player/native-player.js */ "./src/player/native-player.js");
/* harmony import */ var _player_player_events__WEBPACK_IMPORTED_MODULE_5__ = __webpack_require__(/*! ./player/player-events */ "./src/player/player-events.ts");
/* harmony import */ var _player_player_errors_js__WEBPACK_IMPORTED_MODULE_6__ = __webpack_require__(/*! ./player/player-errors.js */ "./src/player/player-errors.js");
/* harmony import */ var _utils_logging_control_js__WEBPACK_IMPORTED_MODULE_7__ = __webpack_require__(/*! ./utils/logging-control.js */ "./src/utils/logging-control.js");
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_8__ = __webpack_require__(/*! ./utils/exception.js */ "./src/utils/exception.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */









// here are all the interfaces
// install polyfills
_utils_polyfill_js__WEBPACK_IMPORTED_MODULE_0__["default"].install();
// factory method
function createPlayer(mediaDataSource, optionalConfig) {
    var mds = mediaDataSource;
    if (mds == null || typeof mds !== 'object') {
        throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_8__["InvalidArgumentException"]('MediaDataSource must be an javascript object!');
    }
    if (!mds.hasOwnProperty('type')) {
        throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_8__["InvalidArgumentException"]('MediaDataSource must has type field to indicate video file type!');
    }
    switch (mds.type) {
        case 'mse':
        case 'mpegts':
        case 'm2ts':
        case 'flv':
            return new _player_mse_player__WEBPACK_IMPORTED_MODULE_3__["default"](mds, optionalConfig);
        default:
            return new _player_native_player_js__WEBPACK_IMPORTED_MODULE_4__["default"](mds, optionalConfig);
    }
}
// feature detection
function isSupported() {
    return _core_features_js__WEBPACK_IMPORTED_MODULE_1__["default"].supportMSEH264Playback();
}
function getFeatureList() {
    return _core_features_js__WEBPACK_IMPORTED_MODULE_1__["default"].getFeatureList();
}
// interfaces
var mpegts = {};
mpegts.createPlayer = createPlayer;
mpegts.isSupported = isSupported;
mpegts.getFeatureList = getFeatureList;
mpegts.BaseLoader = _io_loader_js__WEBPACK_IMPORTED_MODULE_2__["BaseLoader"];
mpegts.LoaderStatus = _io_loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderStatus"];
mpegts.LoaderErrors = _io_loader_js__WEBPACK_IMPORTED_MODULE_2__["LoaderErrors"];
mpegts.Events = _player_player_events__WEBPACK_IMPORTED_MODULE_5__["default"];
mpegts.ErrorTypes = _player_player_errors_js__WEBPACK_IMPORTED_MODULE_6__["ErrorTypes"];
mpegts.ErrorDetails = _player_player_errors_js__WEBPACK_IMPORTED_MODULE_6__["ErrorDetails"];
mpegts.MSEPlayer = _player_mse_player__WEBPACK_IMPORTED_MODULE_3__["default"];
mpegts.NativePlayer = _player_native_player_js__WEBPACK_IMPORTED_MODULE_4__["default"];
mpegts.LoggingControl = _utils_logging_control_js__WEBPACK_IMPORTED_MODULE_7__["default"];
Object.defineProperty(mpegts, 'version', {
    enumerable: true,
    get: function () {
        // replaced by webpack.DefinePlugin
        return "1.8.0";
    }
});
/* harmony default export */ __webpack_exports__["default"] = (mpegts);


/***/ }),

/***/ "./src/player/live-latency-chaser.ts":
/*!*******************************************!*\
  !*** ./src/player/live-latency-chaser.ts ***!
  \*******************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2023 zheng qian. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
// Live buffer latency chaser by directly adjusting HTMLMediaElement.currentTime (not recommended)
var LiveLatencyChaser = /** @class */ (function () {
    function LiveLatencyChaser(config, media_element, on_direct_seek) {
        this._config = null;
        this._media_element = null;
        this._on_direct_seek = null;
        this._config = config;
        this._media_element = media_element;
        this._on_direct_seek = on_direct_seek;
    }
    LiveLatencyChaser.prototype.destroy = function () {
        this._on_direct_seek = null;
        this._media_element = null;
        this._config = null;
    };
    LiveLatencyChaser.prototype.notifyBufferedRangeUpdate = function () {
        this._chaseLiveLatency();
    };
    LiveLatencyChaser.prototype._chaseLiveLatency = function () {
        var buffered = this._media_element.buffered;
        var current_time = this._media_element.currentTime;
        var paused = this._media_element.paused;
        if (!this._config.isLive ||
            !this._config.liveBufferLatencyChasing ||
            buffered.length == 0 ||
            (!this._config.liveBufferLatencyChasingOnPaused && paused)) {
            return;
        }
        var buffered_end = buffered.end(buffered.length - 1);
        if (buffered_end > this._config.liveBufferLatencyMaxLatency) {
            if (buffered_end - current_time > this._config.liveBufferLatencyMaxLatency) {
                var target_time = buffered_end - this._config.liveBufferLatencyMinRemain;
                this._on_direct_seek(target_time);
            }
        }
    };
    return LiveLatencyChaser;
}());
/* harmony default export */ __webpack_exports__["default"] = (LiveLatencyChaser);


/***/ }),

/***/ "./src/player/live-latency-synchronizer.ts":
/*!*************************************************!*\
  !*** ./src/player/live-latency-synchronizer.ts ***!
  \*************************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2023 zheng qian. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
// Live buffer latency synchronizer by increasing HTMLMediaElement.playbackRate
var LiveLatencySynchronizer = /** @class */ (function () {
    function LiveLatencySynchronizer(config, media_element) {
        this._config = null;
        this._media_element = null;
        this.e = null;
        this._config = config;
        this._media_element = media_element;
        this.e = {
            onMediaTimeUpdate: this._onMediaTimeUpdate.bind(this),
        };
        this._media_element.addEventListener('timeupdate', this.e.onMediaTimeUpdate);
    }
    LiveLatencySynchronizer.prototype.destroy = function () {
        this._media_element.removeEventListener('timeupdate', this.e.onMediaTimeUpdate);
        this._media_element = null;
        this._config = null;
    };
    LiveLatencySynchronizer.prototype._onMediaTimeUpdate = function (e) {
        if (!this._config.isLive || !this._config.liveSync) {
            return;
        }
        var latency = this._getCurrentLatency();
        if (latency > this._config.liveSyncMaxLatency) {
            var playback_rate = Math.min(2, Math.max(1, this._config.liveSyncPlaybackRate));
            this._media_element.playbackRate = playback_rate;
        }
        else if (latency > this._config.liveSyncTargetLatency) {
            // do nothing, keep playbackRate
        }
        else if (this._media_element.playbackRate !== 1 && this._media_element.playbackRate !== 0) {
            this._media_element.playbackRate = 1;
        }
    };
    LiveLatencySynchronizer.prototype._getCurrentLatency = function () {
        if (!this._media_element) {
            return 0;
        }
        var buffered = this._media_element.buffered;
        var current_time = this._media_element.currentTime;
        if (buffered.length == 0) {
            return 0;
        }
        var buffered_end = buffered.end(buffered.length - 1);
        return buffered_end - current_time;
    };
    return LiveLatencySynchronizer;
}());
/* harmony default export */ __webpack_exports__["default"] = (LiveLatencySynchronizer);


/***/ }),

/***/ "./src/player/loading-controller.ts":
/*!******************************************!*\
  !*** ./src/player/loading-controller.ts ***!
  \******************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger */ "./src/utils/logger.js");
/*
 * Copyright (C) 2023 zheng qian. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

var LoadingController = /** @class */ (function () {
    function LoadingController(config, media_element, on_pause_transmuxer, on_resume_transmuxer) {
        this.TAG = 'LoadingController';
        this._config = null;
        this._media_element = null;
        this._on_pause_transmuxer = null;
        this._on_resume_transmuxer = null;
        this._paused = false;
        this.e = null;
        this._config = config;
        this._media_element = media_element;
        this._on_pause_transmuxer = on_pause_transmuxer;
        this._on_resume_transmuxer = on_resume_transmuxer;
        this.e = {
            onMediaTimeUpdate: this._onMediaTimeUpdate.bind(this),
        };
    }
    LoadingController.prototype.destroy = function () {
        this._media_element.removeEventListener('timeupdate', this.e.onMediaTimeUpdate);
        this.e = null;
        this._media_element = null;
        this._config = null;
        this._on_pause_transmuxer = null;
        this._on_resume_transmuxer = null;
    };
    // buffered_position: in seconds
    LoadingController.prototype.notifyBufferedPositionChanged = function (buffered_position) {
        if (this._config.isLive || !this._config.lazyLoad) {
            return;
        }
        if (buffered_position == undefined) {
            this._suspendTransmuxerIfNeeded();
        }
        else {
            this._suspendTransmuxerIfBufferedPositionExceeded(buffered_position);
        }
    };
    LoadingController.prototype._onMediaTimeUpdate = function (e) {
        if (this._paused) {
            this._resumeTransmuxerIfNeeded();
        }
    };
    LoadingController.prototype._suspendTransmuxerIfNeeded = function () {
        var buffered = this._media_element.buffered;
        var current_time = this._media_element.currentTime;
        var current_range_end = 0;
        for (var i = 0; i < buffered.length; i++) {
            var start = buffered.start(i);
            var end = buffered.end(i);
            if (start <= current_time && current_time < end) {
                current_range_end = end;
                break;
            }
        }
        if (current_range_end > 0) {
            this._suspendTransmuxerIfBufferedPositionExceeded(current_range_end);
        }
    };
    LoadingController.prototype._suspendTransmuxerIfBufferedPositionExceeded = function (buffered_end) {
        var current_time = this._media_element.currentTime;
        if (buffered_end >= current_time + this._config.lazyLoadMaxDuration && !this._paused) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Maximum buffering duration exceeded, suspend transmuxing task');
            this.suspendTransmuxer();
            this._media_element.addEventListener('timeupdate', this.e.onMediaTimeUpdate);
        }
    };
    LoadingController.prototype.suspendTransmuxer = function () {
        this._paused = true;
        this._on_pause_transmuxer();
    };
    LoadingController.prototype._resumeTransmuxerIfNeeded = function () {
        var buffered = this._media_element.buffered;
        var current_time = this._media_element.currentTime;
        var recover_duration = this._config.lazyLoadRecoverDuration;
        var should_resume = false;
        for (var i = 0; i < buffered.length; i++) {
            var from = buffered.start(i);
            var to = buffered.end(i);
            if (current_time >= from && current_time < to) {
                if (current_time >= to - recover_duration) {
                    should_resume = true;
                }
                break;
            }
        }
        if (should_resume) {
            _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, 'Continue loading from paused position');
            this.resumeTransmuxer();
            this._media_element.removeEventListener('timeupdate', this.e.onMediaTimeUpdate);
        }
    };
    LoadingController.prototype.resumeTransmuxer = function () {
        this._paused = false;
        this._on_resume_transmuxer();
    };
    return LoadingController;
}());
/* harmony default export */ __webpack_exports__["default"] = (LoadingController);


/***/ }),

/***/ "./src/player/mse-player.ts":
/*!**********************************!*\
  !*** ./src/player/mse-player.ts ***!
  \**********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger */ "./src/utils/logger.js");
/* harmony import */ var _player_engine_main_thread__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ./player-engine-main-thread */ "./src/player/player-engine-main-thread.ts");
/* harmony import */ var _player_engine_dedicated_thread__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ./player-engine-dedicated-thread */ "./src/player/player-engine-dedicated-thread.ts");
/* harmony import */ var _utils_exception__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ../utils/exception */ "./src/utils/exception.js");
/*
 * Copyright (C) 2023 zheng qian. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */




var MSEPlayer = /** @class */ (function () {
    function MSEPlayer(mediaDataSource, config) {
        this.TAG = 'MSEPlayer';
        this._type = 'MSEPlayer';
        this._media_element = null;
        this._player_engine = null;
        var typeLowerCase = mediaDataSource.type.toLowerCase();
        if (typeLowerCase !== 'mse'
            && typeLowerCase !== 'mpegts'
            && typeLowerCase !== 'm2ts'
            && typeLowerCase !== 'flv') {
            throw new _utils_exception__WEBPACK_IMPORTED_MODULE_3__["InvalidArgumentException"]('MSEPlayer requires an mpegts/m2ts/flv MediaDataSource input!');
        }
        if (config && config.enableWorkerForMSE && _player_engine_dedicated_thread__WEBPACK_IMPORTED_MODULE_2__["default"].isSupported()) {
            try {
                this._player_engine = new _player_engine_dedicated_thread__WEBPACK_IMPORTED_MODULE_2__["default"](mediaDataSource, config);
            }
            catch (error) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].e(this.TAG, 'Error while initializing PlayerEngineDedicatedThread, fallback to PlayerEngineMainThread');
                this._player_engine = new _player_engine_main_thread__WEBPACK_IMPORTED_MODULE_1__["default"](mediaDataSource, config);
            }
        }
        else {
            this._player_engine = new _player_engine_main_thread__WEBPACK_IMPORTED_MODULE_1__["default"](mediaDataSource, config);
        }
    }
    MSEPlayer.prototype.destroy = function () {
        this._player_engine.destroy();
        this._player_engine = null;
        this._media_element = null;
    };
    MSEPlayer.prototype.on = function (event, listener) {
        this._player_engine.on(event, listener);
    };
    MSEPlayer.prototype.off = function (event, listener) {
        this._player_engine.off(event, listener);
    };
    MSEPlayer.prototype.attachMediaElement = function (mediaElement) {
        this._media_element = mediaElement;
        this._player_engine.attachMediaElement(mediaElement);
    };
    MSEPlayer.prototype.detachMediaElement = function () {
        this._media_element = null;
        this._player_engine.detachMediaElement();
    };
    MSEPlayer.prototype.load = function () {
        this._player_engine.load();
    };
    MSEPlayer.prototype.unload = function () {
        this._player_engine.unload();
    };
    MSEPlayer.prototype.play = function () {
        return this._player_engine.play();
    };
    MSEPlayer.prototype.pause = function () {
        this._player_engine.pause();
    };
    Object.defineProperty(MSEPlayer.prototype, "type", {
        get: function () {
            return this._type;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(MSEPlayer.prototype, "buffered", {
        get: function () {
            return this._media_element.buffered;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(MSEPlayer.prototype, "duration", {
        get: function () {
            return this._media_element.duration;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(MSEPlayer.prototype, "volume", {
        get: function () {
            return this._media_element.volume;
        },
        set: function (value) {
            this._media_element.volume = value;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(MSEPlayer.prototype, "muted", {
        get: function () {
            return this._media_element.muted;
        },
        set: function (muted) {
            this._media_element.muted = muted;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(MSEPlayer.prototype, "currentTime", {
        get: function () {
            if (this._media_element) {
                return this._media_element.currentTime;
            }
            return 0;
        },
        set: function (seconds) {
            this._player_engine.seek(seconds);
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(MSEPlayer.prototype, "mediaInfo", {
        get: function () {
            return this._player_engine.mediaInfo;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(MSEPlayer.prototype, "statisticsInfo", {
        get: function () {
            return this._player_engine.statisticsInfo;
        },
        enumerable: false,
        configurable: true
    });
    return MSEPlayer;
}());
/* harmony default export */ __webpack_exports__["default"] = (MSEPlayer);


/***/ }),

/***/ "./src/player/native-player.js":
/*!*************************************!*\
  !*** ./src/player/native-player.js ***!
  \*************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! events */ "./node_modules/events/events.js");
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0___default = /*#__PURE__*/__webpack_require__.n(events__WEBPACK_IMPORTED_MODULE_0__);
/* harmony import */ var _player_events__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ./player-events */ "./src/player/player-events.ts");
/* harmony import */ var _config_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ../config.js */ "./src/config.js");
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ../utils/exception.js */ "./src/utils/exception.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */




// Player wrapper for browser's native player (HTMLVideoElement) without MediaSource src.
var NativePlayer = /** @class */ (function () {
    function NativePlayer(mediaDataSource, config) {
        this.TAG = 'NativePlayer';
        this._type = 'NativePlayer';
        this._emitter = new events__WEBPACK_IMPORTED_MODULE_0___default.a();
        this._config = Object(_config_js__WEBPACK_IMPORTED_MODULE_2__["createDefaultConfig"])();
        if (typeof config === 'object') {
            Object.assign(this._config, config);
        }
        var typeLowerCase = mediaDataSource.type.toLowerCase();
        if (typeLowerCase === 'mse'
            || typeLowerCase === 'mpegts'
            || typeLowerCase === 'm2ts'
            || typeLowerCase === 'flv') {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_3__["InvalidArgumentException"]('NativePlayer does\'t support mse/mpegts/m2ts/flv MediaDataSource input!');
        }
        if (mediaDataSource.hasOwnProperty('segments')) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_3__["InvalidArgumentException"]("NativePlayer(".concat(mediaDataSource.type, ") doesn't support multipart playback!"));
        }
        this.e = {
            onvLoadedMetadata: this._onvLoadedMetadata.bind(this)
        };
        this._pendingSeekTime = null;
        this._statisticsReporter = null;
        this._mediaDataSource = mediaDataSource;
        this._mediaElement = null;
    }
    NativePlayer.prototype.destroy = function () {
        this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_1__["default"].DESTROYING);
        if (this._mediaElement) {
            this.unload();
            this.detachMediaElement();
        }
        this.e = null;
        this._mediaDataSource = null;
        this._emitter.removeAllListeners();
        this._emitter = null;
    };
    NativePlayer.prototype.on = function (event, listener) {
        var _this = this;
        if (event === _player_events__WEBPACK_IMPORTED_MODULE_1__["default"].MEDIA_INFO) {
            if (this._mediaElement != null && this._mediaElement.readyState !== 0) { // HAVE_NOTHING
                Promise.resolve().then(function () {
                    _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_1__["default"].MEDIA_INFO, _this.mediaInfo);
                });
            }
        }
        else if (event === _player_events__WEBPACK_IMPORTED_MODULE_1__["default"].STATISTICS_INFO) {
            if (this._mediaElement != null && this._mediaElement.readyState !== 0) {
                Promise.resolve().then(function () {
                    _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_1__["default"].STATISTICS_INFO, _this.statisticsInfo);
                });
            }
        }
        this._emitter.addListener(event, listener);
    };
    NativePlayer.prototype.off = function (event, listener) {
        this._emitter.removeListener(event, listener);
    };
    NativePlayer.prototype.attachMediaElement = function (mediaElement) {
        this._mediaElement = mediaElement;
        mediaElement.addEventListener('loadedmetadata', this.e.onvLoadedMetadata);
        if (this._pendingSeekTime != null) {
            try {
                mediaElement.currentTime = this._pendingSeekTime;
                this._pendingSeekTime = null;
            }
            catch (e) {
                // IE11 may throw InvalidStateError if readyState === 0
                // Defer set currentTime operation after loadedmetadata
            }
        }
    };
    NativePlayer.prototype.detachMediaElement = function () {
        if (this._mediaElement) {
            this._mediaElement.src = '';
            this._mediaElement.removeAttribute('src');
            this._mediaElement.removeEventListener('loadedmetadata', this.e.onvLoadedMetadata);
            this._mediaElement = null;
        }
        if (this._statisticsReporter != null) {
            window.clearInterval(this._statisticsReporter);
            this._statisticsReporter = null;
        }
    };
    NativePlayer.prototype.load = function () {
        if (!this._mediaElement) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_3__["IllegalStateException"]('HTMLMediaElement must be attached before load()!');
        }
        this._mediaElement.src = this._mediaDataSource.url;
        if (this._mediaElement.readyState > 0) {
            this._mediaElement.currentTime = 0;
        }
        this._mediaElement.preload = 'auto';
        this._mediaElement.load();
        this._statisticsReporter = window.setInterval(this._reportStatisticsInfo.bind(this), this._config.statisticsInfoReportInterval);
    };
    NativePlayer.prototype.unload = function () {
        if (this._mediaElement) {
            this._mediaElement.src = '';
            this._mediaElement.removeAttribute('src');
        }
        if (this._statisticsReporter != null) {
            window.clearInterval(this._statisticsReporter);
            this._statisticsReporter = null;
        }
    };
    NativePlayer.prototype.play = function () {
        return this._mediaElement.play();
    };
    NativePlayer.prototype.pause = function () {
        this._mediaElement.pause();
    };
    Object.defineProperty(NativePlayer.prototype, "type", {
        get: function () {
            return this._type;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(NativePlayer.prototype, "buffered", {
        get: function () {
            return this._mediaElement.buffered;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(NativePlayer.prototype, "duration", {
        get: function () {
            return this._mediaElement.duration;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(NativePlayer.prototype, "volume", {
        get: function () {
            return this._mediaElement.volume;
        },
        set: function (value) {
            this._mediaElement.volume = value;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(NativePlayer.prototype, "muted", {
        get: function () {
            return this._mediaElement.muted;
        },
        set: function (muted) {
            this._mediaElement.muted = muted;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(NativePlayer.prototype, "currentTime", {
        get: function () {
            if (this._mediaElement) {
                return this._mediaElement.currentTime;
            }
            return 0;
        },
        set: function (seconds) {
            if (this._mediaElement) {
                this._mediaElement.currentTime = seconds;
            }
            else {
                this._pendingSeekTime = seconds;
            }
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(NativePlayer.prototype, "mediaInfo", {
        get: function () {
            var mediaPrefix = (this._mediaElement instanceof HTMLAudioElement) ? 'audio/' : 'video/';
            var info = {
                mimeType: mediaPrefix + this._mediaDataSource.type
            };
            if (this._mediaElement) {
                info.duration = Math.floor(this._mediaElement.duration * 1000);
                if (this._mediaElement instanceof HTMLVideoElement) {
                    info.width = this._mediaElement.videoWidth;
                    info.height = this._mediaElement.videoHeight;
                }
            }
            return info;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(NativePlayer.prototype, "statisticsInfo", {
        get: function () {
            var info = {
                playerType: this._type,
                url: this._mediaDataSource.url
            };
            if (!(this._mediaElement instanceof HTMLVideoElement)) {
                return info;
            }
            var hasQualityInfo = true;
            var decoded = 0;
            var dropped = 0;
            if (this._mediaElement.getVideoPlaybackQuality) {
                var quality = this._mediaElement.getVideoPlaybackQuality();
                decoded = quality.totalVideoFrames;
                dropped = quality.droppedVideoFrames;
            }
            else if (this._mediaElement.webkitDecodedFrameCount != undefined) {
                decoded = this._mediaElement.webkitDecodedFrameCount;
                dropped = this._mediaElement.webkitDroppedFrameCount;
            }
            else {
                hasQualityInfo = false;
            }
            if (hasQualityInfo) {
                info.decodedFrames = decoded;
                info.droppedFrames = dropped;
            }
            return info;
        },
        enumerable: false,
        configurable: true
    });
    NativePlayer.prototype._onvLoadedMetadata = function (e) {
        if (this._pendingSeekTime != null) {
            this._mediaElement.currentTime = this._pendingSeekTime;
            this._pendingSeekTime = null;
        }
        this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_1__["default"].MEDIA_INFO, this.mediaInfo);
    };
    NativePlayer.prototype._reportStatisticsInfo = function () {
        this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_1__["default"].STATISTICS_INFO, this.statisticsInfo);
    };
    return NativePlayer;
}());
/* harmony default export */ __webpack_exports__["default"] = (NativePlayer);


/***/ }),

/***/ "./src/player/player-engine-dedicated-thread.ts":
/*!******************************************************!*\
  !*** ./src/player/player-engine-dedicated-thread.ts ***!
  \******************************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! events */ "./node_modules/events/events.js");
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0___default = /*#__PURE__*/__webpack_require__.n(events__WEBPACK_IMPORTED_MODULE_0__);
/* harmony import */ var _utils_webworkify_webpack__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ../utils/webworkify-webpack */ "./src/utils/webworkify-webpack.js");
/* harmony import */ var _utils_webworkify_webpack__WEBPACK_IMPORTED_MODULE_1___default = /*#__PURE__*/__webpack_require__.n(_utils_webworkify_webpack__WEBPACK_IMPORTED_MODULE_1__);
/* harmony import */ var _utils_logger__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ../utils/logger */ "./src/utils/logger.js");
/* harmony import */ var _utils_logging_control_js__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ../utils/logging-control.js */ "./src/utils/logging-control.js");
/* harmony import */ var _config__WEBPACK_IMPORTED_MODULE_4__ = __webpack_require__(/*! ../config */ "./src/config.js");
/* harmony import */ var _core_mse_events__WEBPACK_IMPORTED_MODULE_5__ = __webpack_require__(/*! ../core/mse-events */ "./src/core/mse-events.ts");
/* harmony import */ var _player_events__WEBPACK_IMPORTED_MODULE_6__ = __webpack_require__(/*! ./player-events */ "./src/player/player-events.ts");
/* harmony import */ var _core_transmuxing_events__WEBPACK_IMPORTED_MODULE_7__ = __webpack_require__(/*! ../core/transmuxing-events */ "./src/core/transmuxing-events.ts");
/* harmony import */ var _seeking_handler__WEBPACK_IMPORTED_MODULE_8__ = __webpack_require__(/*! ./seeking-handler */ "./src/player/seeking-handler.ts");
/* harmony import */ var _loading_controller__WEBPACK_IMPORTED_MODULE_9__ = __webpack_require__(/*! ./loading-controller */ "./src/player/loading-controller.ts");
/* harmony import */ var _startup_stall_jumper__WEBPACK_IMPORTED_MODULE_10__ = __webpack_require__(/*! ./startup-stall-jumper */ "./src/player/startup-stall-jumper.ts");
/* harmony import */ var _live_latency_chaser__WEBPACK_IMPORTED_MODULE_11__ = __webpack_require__(/*! ./live-latency-chaser */ "./src/player/live-latency-chaser.ts");
/* harmony import */ var _live_latency_synchronizer__WEBPACK_IMPORTED_MODULE_12__ = __webpack_require__(/*! ./live-latency-synchronizer */ "./src/player/live-latency-synchronizer.ts");
/*
 * Copyright (C) 2023 zheng qian. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */













var PlayerEngineDedicatedThread = /** @class */ (function () {
    function PlayerEngineDedicatedThread(mediaDataSource, config) {
        this.TAG = 'PlayerEngineDedicatedThread';
        this._emitter = new events__WEBPACK_IMPORTED_MODULE_0__();
        this._media_element = null;
        this._worker_destroying = false;
        this._seeking_handler = null;
        this._loading_controller = null;
        this._startup_stall_jumper = null;
        this._live_latency_chaser = null;
        this._live_latency_synchronizer = null;
        this._pending_seek_time = null;
        this._media_info = null;
        this._statistics_info = null;
        this.e = null;
        this._media_data_source = mediaDataSource;
        this._config = Object(_config__WEBPACK_IMPORTED_MODULE_4__["createDefaultConfig"])();
        if (typeof config === 'object') {
            Object.assign(this._config, config);
        }
        if (mediaDataSource.isLive === true) {
            this._config.isLive = true;
        }
        this.e = {
            onLoggingConfigChanged: this._onLoggingConfigChanged.bind(this),
            onMediaLoadedMetadata: this._onMediaLoadedMetadata.bind(this),
            onMediaTimeUpdate: this._onMediaTimeUpdate.bind(this),
            onMediaReadyStateChanged: this._onMediaReadyStateChange.bind(this),
        };
        _utils_logging_control_js__WEBPACK_IMPORTED_MODULE_3__["default"].registerListener(this.e.onLoggingConfigChanged);
        this._worker = _utils_webworkify_webpack__WEBPACK_IMPORTED_MODULE_1__(/*require.resolve*/(/*! ./player-engine-worker */ "./src/player/player-engine-worker.ts"), { all: true });
        this._worker.addEventListener('message', this._onWorkerMessage.bind(this));
        this._worker.postMessage({
            cmd: 'init',
            media_data_source: this._media_data_source,
            config: this._config
        });
        this._worker.postMessage({
            cmd: 'logging_config',
            logging_config: _utils_logging_control_js__WEBPACK_IMPORTED_MODULE_3__["default"].getConfig()
        });
    }
    PlayerEngineDedicatedThread.isSupported = function () {
        if (!self.Worker) {
            return false;
        }
        if (self.MediaSource &&
            ('canConstructInDedicatedWorker' in self.MediaSource) &&
            (self.MediaSource['canConstructInDedicatedWorker'] === true)) {
            return true;
        }
        if (self.ManagedMediaSource &&
            ('canConstructInDedicatedWorker' in self.ManagedMediaSource) &&
            (self.ManagedMediaSource['canConstructInDedicatedWorker'] === true)) {
            return true;
        }
        return false;
    };
    PlayerEngineDedicatedThread.prototype.destroy = function () {
        this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_6__["default"].DESTROYING);
        this.unload();
        this.detachMediaElement();
        this._worker_destroying = true;
        this._worker.postMessage({
            cmd: 'destroy'
        });
        _utils_logging_control_js__WEBPACK_IMPORTED_MODULE_3__["default"].removeListener(this.e.onLoggingConfigChanged);
        this.e = null;
        this._media_data_source = null;
        this._emitter.removeAllListeners();
        this._emitter = null;
    };
    PlayerEngineDedicatedThread.prototype.on = function (event, listener) {
        var _this = this;
        this._emitter.addListener(event, listener);
        // For media_info / statistics_info event, trigger it immediately
        if (event === _player_events__WEBPACK_IMPORTED_MODULE_6__["default"].MEDIA_INFO && this._media_info) {
            Promise.resolve().then(function () { return _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_6__["default"].MEDIA_INFO, _this.mediaInfo); });
        }
        else if (event == _player_events__WEBPACK_IMPORTED_MODULE_6__["default"].STATISTICS_INFO && this._statistics_info) {
            Promise.resolve().then(function () { return _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_6__["default"].STATISTICS_INFO, _this.statisticsInfo); });
        }
    };
    PlayerEngineDedicatedThread.prototype.off = function (event, listener) {
        this._emitter.removeListener(event, listener);
    };
    PlayerEngineDedicatedThread.prototype.attachMediaElement = function (mediaElement) {
        this._media_element = mediaElement;
        // Remove src / srcObject of HTMLMediaElement for cleanup
        this._media_element.src = '';
        this._media_element.removeAttribute('src');
        this._media_element.srcObject = null;
        this._media_element.load();
        this._media_element.addEventListener('loadedmetadata', this.e.onMediaLoadedMetadata);
        this._media_element.addEventListener('timeupdate', this.e.onMediaTimeUpdate);
        this._media_element.addEventListener('readystatechange', this.e.onMediaReadyStateChanged);
        this._worker.postMessage({
            cmd: 'initialize_mse',
        });
        // Then wait for 'mse_init' message from worker to receive MediaSource handle
    };
    PlayerEngineDedicatedThread.prototype.detachMediaElement = function () {
        this._worker.postMessage({
            cmd: 'shutdown_mse',
        });
        if (this._media_element) {
            // Remove all appended event listeners
            this._media_element.removeEventListener('loadedmetadata', this.e.onMediaLoadedMetadata);
            this._media_element.removeEventListener('timeupdate', this.e.onMediaTimeUpdate);
            this._media_element.removeEventListener('readystatechange', this.e.onMediaReadyStateChanged);
            // Detach media source from media element
            this._media_element.src = '';
            this._media_element.removeAttribute('src');
            this._media_element.srcObject = null;
            this._media_element.load();
            this._media_element = null;
        }
    };
    PlayerEngineDedicatedThread.prototype.load = function () {
        this._worker.postMessage({
            cmd: 'load',
        });
        this._seeking_handler = new _seeking_handler__WEBPACK_IMPORTED_MODULE_8__["default"](this._config, this._media_element, this._onRequiredUnbufferedSeek.bind(this));
        this._loading_controller = new _loading_controller__WEBPACK_IMPORTED_MODULE_9__["default"](this._config, this._media_element, this._onRequestPauseTransmuxer.bind(this), this._onRequestResumeTransmuxer.bind(this));
        this._startup_stall_jumper = new _startup_stall_jumper__WEBPACK_IMPORTED_MODULE_10__["default"](this._media_element, this._onRequestDirectSeek.bind(this));
        if (this._config.isLive && this._config.liveBufferLatencyChasing) {
            this._live_latency_chaser = new _live_latency_chaser__WEBPACK_IMPORTED_MODULE_11__["default"](this._config, this._media_element, this._onRequestDirectSeek.bind(this));
        }
        if (this._config.isLive && this._config.liveSync) {
            this._live_latency_synchronizer = new _live_latency_synchronizer__WEBPACK_IMPORTED_MODULE_12__["default"](this._config, this._media_element);
        }
        // Reset currentTime to 0
        if (this._media_element.readyState > 0) {
            // IE11 may throw InvalidStateError if readyState === 0
            this._seeking_handler.directSeek(0);
        }
    };
    PlayerEngineDedicatedThread.prototype.unload = function () {
        var _a, _b, _c, _d, _e, _f;
        (_a = this._media_element) === null || _a === void 0 ? void 0 : _a.pause();
        this._worker.postMessage({
            cmd: 'unload',
        });
        (_b = this._live_latency_synchronizer) === null || _b === void 0 ? void 0 : _b.destroy();
        this._live_latency_synchronizer = null;
        (_c = this._live_latency_chaser) === null || _c === void 0 ? void 0 : _c.destroy();
        this._live_latency_chaser = null;
        (_d = this._startup_stall_jumper) === null || _d === void 0 ? void 0 : _d.destroy();
        this._startup_stall_jumper = null;
        (_e = this._loading_controller) === null || _e === void 0 ? void 0 : _e.destroy();
        this._loading_controller = null;
        (_f = this._seeking_handler) === null || _f === void 0 ? void 0 : _f.destroy();
        this._seeking_handler = null;
    };
    PlayerEngineDedicatedThread.prototype.play = function () {
        return this._media_element.play();
    };
    PlayerEngineDedicatedThread.prototype.pause = function () {
        this._media_element.pause();
    };
    PlayerEngineDedicatedThread.prototype.seek = function (seconds) {
        if (this._media_element && this._seeking_handler) {
            this._seeking_handler.seek(seconds);
        }
        else {
            this._pending_seek_time = seconds;
        }
    };
    Object.defineProperty(PlayerEngineDedicatedThread.prototype, "mediaInfo", {
        get: function () {
            return Object.assign({}, this._media_info);
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(PlayerEngineDedicatedThread.prototype, "statisticsInfo", {
        get: function () {
            return Object.assign({}, this._statistics_info);
        },
        enumerable: false,
        configurable: true
    });
    PlayerEngineDedicatedThread.prototype._onLoggingConfigChanged = function (config) {
        var _a;
        (_a = this._worker) === null || _a === void 0 ? void 0 : _a.postMessage({
            cmd: 'logging_config',
            logging_config: config,
        });
    };
    PlayerEngineDedicatedThread.prototype._onMSEUpdateEnd = function () {
        if (this._config.isLive && this._config.liveBufferLatencyChasing && this._live_latency_chaser) {
            this._live_latency_chaser.notifyBufferedRangeUpdate();
        }
        this._loading_controller.notifyBufferedPositionChanged();
    };
    PlayerEngineDedicatedThread.prototype._onMSEBufferFull = function () {
        _utils_logger__WEBPACK_IMPORTED_MODULE_2__["default"].v(this.TAG, 'MSE SourceBuffer is full, suspend transmuxing task');
        this._loading_controller.suspendTransmuxer();
    };
    PlayerEngineDedicatedThread.prototype._onMediaLoadedMetadata = function (e) {
        if (this._pending_seek_time != null) {
            this._seeking_handler.seek(this._pending_seek_time);
            this._pending_seek_time = null;
        }
    };
    PlayerEngineDedicatedThread.prototype._onRequestDirectSeek = function (target) {
        this._seeking_handler.directSeek(target);
    };
    PlayerEngineDedicatedThread.prototype._onRequiredUnbufferedSeek = function (milliseconds) {
        this._worker.postMessage({
            cmd: 'unbuffered_seek',
            milliseconds: milliseconds
        });
    };
    PlayerEngineDedicatedThread.prototype._onRequestPauseTransmuxer = function () {
        this._worker.postMessage({
            cmd: 'pause_transmuxer'
        });
    };
    PlayerEngineDedicatedThread.prototype._onRequestResumeTransmuxer = function () {
        this._worker.postMessage({
            cmd: 'resume_transmuxer'
        });
    };
    PlayerEngineDedicatedThread.prototype._onMediaTimeUpdate = function (e) {
        this._worker.postMessage({
            cmd: 'timeupdate',
            current_time: e.target.currentTime,
        });
    };
    PlayerEngineDedicatedThread.prototype._onMediaReadyStateChange = function (e) {
        this._worker.postMessage({
            cmd: 'readystatechange',
            ready_state: e.target.readyState,
        });
    };
    PlayerEngineDedicatedThread.prototype._onWorkerMessage = function (e) {
        var _a;
        var message_packet = e.data;
        var msg = message_packet.msg;
        if (msg == 'destroyed' || this._worker_destroying) {
            this._worker_destroying = false;
            (_a = this._worker) === null || _a === void 0 ? void 0 : _a.terminate();
            this._worker = null;
            return;
        }
        switch (msg) {
            case 'mse_init': {
                var packet = message_packet;
                // Use ManagedMediaSource only if w3c MediaSource is not available (e.g. iOS Safari)
                var use_managed_media_source = ('ManagedMediaSource' in self) && !('MediaSource' in self);
                if (use_managed_media_source) {
                    // When using ManagedMediaSource, MediaSource will not open unless disableRemotePlayback is set to true
                    this._media_element['disableRemotePlayback'] = true;
                }
                // Attach to HTMLMediaElement by using MediaSource Handle
                this._media_element.srcObject = packet.handle;
                break;
            }
            case 'mse_event': {
                var packet = message_packet;
                if (packet.event == _core_mse_events__WEBPACK_IMPORTED_MODULE_5__["default"].UPDATE_END) {
                    this._onMSEUpdateEnd();
                }
                else if (packet.event == _core_mse_events__WEBPACK_IMPORTED_MODULE_5__["default"].BUFFER_FULL) {
                    this._onMSEBufferFull();
                }
                break;
            }
            case 'transmuxing_event': {
                var packet = message_packet;
                if (packet.event == _core_transmuxing_events__WEBPACK_IMPORTED_MODULE_7__["default"].MEDIA_INFO) {
                    var packet_1 = message_packet;
                    this._media_info = packet_1.info;
                    this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_6__["default"].MEDIA_INFO, Object.assign({}, packet_1.info));
                }
                else if (packet.event == _core_transmuxing_events__WEBPACK_IMPORTED_MODULE_7__["default"].STATISTICS_INFO) {
                    var packet_2 = message_packet;
                    this._statistics_info = this._fillStatisticsInfo(packet_2.info);
                    this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_6__["default"].STATISTICS_INFO, Object.assign({}, packet_2.info));
                }
                else if (packet.event == _core_transmuxing_events__WEBPACK_IMPORTED_MODULE_7__["default"].RECOMMEND_SEEKPOINT) {
                    var packet_3 = message_packet;
                    if (this._media_element && !this._config.accurateSeek) {
                        this._seeking_handler.directSeek(packet_3.milliseconds / 1000);
                    }
                }
                break;
            }
            case 'player_event': {
                var packet = message_packet;
                if (packet.event == _player_events__WEBPACK_IMPORTED_MODULE_6__["default"].ERROR) {
                    var packet_4 = message_packet;
                    this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_6__["default"].ERROR, packet_4.error_type, packet_4.error_detail, packet_4.info);
                }
                else if ('extraData' in packet) {
                    var packet_5 = message_packet;
                    this._emitter.emit(packet_5.event, packet_5.extraData);
                }
                break;
            }
            case 'logcat_callback': {
                var packet = message_packet;
                _utils_logger__WEBPACK_IMPORTED_MODULE_2__["default"].emitter.emit('log', packet.type, packet.logcat);
                break;
            }
            case 'buffered_position_changed': {
                var packet = message_packet;
                this._loading_controller.notifyBufferedPositionChanged(packet.buffered_position_milliseconds / 1000);
                break;
            }
        }
    };
    PlayerEngineDedicatedThread.prototype._fillStatisticsInfo = function (stat_info) {
        stat_info.playerType = 'MSEPlayer';
        if (!(this._media_element instanceof HTMLVideoElement)) {
            return stat_info;
        }
        var has_quality_info = true;
        var decoded = 0;
        var dropped = 0;
        if (this._media_element.getVideoPlaybackQuality) {
            var quality = this._media_element.getVideoPlaybackQuality();
            decoded = quality.totalVideoFrames;
            dropped = quality.droppedVideoFrames;
        }
        else if (this._media_element['webkitDecodedFrameCount'] != undefined) {
            decoded = this._media_element['webkitDecodedFrameCount'];
            dropped = this._media_element['webkitDroppedFrameCount'];
        }
        else {
            has_quality_info = false;
        }
        if (has_quality_info) {
            stat_info.decodedFrames = decoded;
            stat_info.droppedFrames = dropped;
        }
        return stat_info;
    };
    return PlayerEngineDedicatedThread;
}());
/* harmony default export */ __webpack_exports__["default"] = (PlayerEngineDedicatedThread);


/***/ }),

/***/ "./src/player/player-engine-main-thread.ts":
/*!*************************************************!*\
  !*** ./src/player/player-engine-main-thread.ts ***!
  \*************************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! events */ "./node_modules/events/events.js");
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0___default = /*#__PURE__*/__webpack_require__.n(events__WEBPACK_IMPORTED_MODULE_0__);
/* harmony import */ var _utils_logger__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ../utils/logger */ "./src/utils/logger.js");
/* harmony import */ var _config__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ../config */ "./src/config.js");
/* harmony import */ var _core_mse_controller__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ../core/mse-controller */ "./src/core/mse-controller.js");
/* harmony import */ var _player_events__WEBPACK_IMPORTED_MODULE_4__ = __webpack_require__(/*! ./player-events */ "./src/player/player-events.ts");
/* harmony import */ var _core_transmuxer__WEBPACK_IMPORTED_MODULE_5__ = __webpack_require__(/*! ../core/transmuxer */ "./src/core/transmuxer.js");
/* harmony import */ var _core_mse_events__WEBPACK_IMPORTED_MODULE_6__ = __webpack_require__(/*! ../core/mse-events */ "./src/core/mse-events.ts");
/* harmony import */ var _player_errors__WEBPACK_IMPORTED_MODULE_7__ = __webpack_require__(/*! ./player-errors */ "./src/player/player-errors.js");
/* harmony import */ var _utils_exception__WEBPACK_IMPORTED_MODULE_8__ = __webpack_require__(/*! ../utils/exception */ "./src/utils/exception.js");
/* harmony import */ var _core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__ = __webpack_require__(/*! ../core/transmuxing-events */ "./src/core/transmuxing-events.ts");
/* harmony import */ var _seeking_handler__WEBPACK_IMPORTED_MODULE_10__ = __webpack_require__(/*! ./seeking-handler */ "./src/player/seeking-handler.ts");
/* harmony import */ var _loading_controller__WEBPACK_IMPORTED_MODULE_11__ = __webpack_require__(/*! ./loading-controller */ "./src/player/loading-controller.ts");
/* harmony import */ var _startup_stall_jumper__WEBPACK_IMPORTED_MODULE_12__ = __webpack_require__(/*! ./startup-stall-jumper */ "./src/player/startup-stall-jumper.ts");
/* harmony import */ var _live_latency_chaser__WEBPACK_IMPORTED_MODULE_13__ = __webpack_require__(/*! ./live-latency-chaser */ "./src/player/live-latency-chaser.ts");
/* harmony import */ var _live_latency_synchronizer__WEBPACK_IMPORTED_MODULE_14__ = __webpack_require__(/*! ./live-latency-synchronizer */ "./src/player/live-latency-synchronizer.ts");
/*
 * Copyright (C) 2023 zheng qian. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */















var PlayerEngineMainThread = /** @class */ (function () {
    function PlayerEngineMainThread(mediaDataSource, config) {
        this.TAG = 'PlayerEngineMainThread';
        this._emitter = new events__WEBPACK_IMPORTED_MODULE_0__();
        this._media_element = null;
        this._mse_controller = null;
        this._transmuxer = null;
        this._pending_seek_time = null;
        this._seeking_handler = null;
        this._loading_controller = null;
        this._startup_stall_jumper = null;
        this._live_latency_chaser = null;
        this._live_latency_synchronizer = null;
        this._mse_source_opened = false;
        this._has_pending_load = false;
        this._loaded_metadata_received = false;
        this._media_info = null;
        this._statistics_info = null;
        this.e = null;
        this._media_data_source = mediaDataSource;
        this._config = Object(_config__WEBPACK_IMPORTED_MODULE_2__["createDefaultConfig"])();
        if (typeof config === 'object') {
            Object.assign(this._config, config);
        }
        if (mediaDataSource.isLive === true) {
            this._config.isLive = true;
        }
        this.e = {
            onMediaLoadedMetadata: this._onMediaLoadedMetadata.bind(this),
        };
    }
    PlayerEngineMainThread.prototype.destroy = function () {
        this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].DESTROYING);
        if (this._transmuxer) {
            this.unload();
        }
        if (this._media_element) {
            this.detachMediaElement();
        }
        this.e = null;
        this._media_data_source = null;
        this._emitter.removeAllListeners();
        this._emitter = null;
    };
    PlayerEngineMainThread.prototype.on = function (event, listener) {
        var _this = this;
        this._emitter.addListener(event, listener);
        // For media_info / statistics_info event, trigger it immediately
        if (event === _player_events__WEBPACK_IMPORTED_MODULE_4__["default"].MEDIA_INFO && this._media_info) {
            Promise.resolve().then(function () { return _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].MEDIA_INFO, _this.mediaInfo); });
        }
        else if (event == _player_events__WEBPACK_IMPORTED_MODULE_4__["default"].STATISTICS_INFO && this._statistics_info) {
            Promise.resolve().then(function () { return _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].STATISTICS_INFO, _this.statisticsInfo); });
        }
    };
    PlayerEngineMainThread.prototype.off = function (event, listener) {
        this._emitter.removeListener(event, listener);
    };
    PlayerEngineMainThread.prototype.attachMediaElement = function (mediaElement) {
        var _this = this;
        this._media_element = mediaElement;
        // Remove src / srcObject of HTMLMediaElement for cleanup
        mediaElement.src = '';
        mediaElement.removeAttribute('src');
        mediaElement.srcObject = null;
        mediaElement.load();
        mediaElement.addEventListener('loadedmetadata', this.e.onMediaLoadedMetadata);
        this._mse_controller = new _core_mse_controller__WEBPACK_IMPORTED_MODULE_3__["default"](this._config);
        this._mse_controller.on(_core_mse_events__WEBPACK_IMPORTED_MODULE_6__["default"].UPDATE_END, this._onMSEUpdateEnd.bind(this));
        this._mse_controller.on(_core_mse_events__WEBPACK_IMPORTED_MODULE_6__["default"].BUFFER_FULL, this._onMSEBufferFull.bind(this));
        this._mse_controller.on(_core_mse_events__WEBPACK_IMPORTED_MODULE_6__["default"].SOURCE_OPEN, this._onMSESourceOpen.bind(this));
        this._mse_controller.on(_core_mse_events__WEBPACK_IMPORTED_MODULE_6__["default"].ERROR, this._onMSEError.bind(this));
        this._mse_controller.on(_core_mse_events__WEBPACK_IMPORTED_MODULE_6__["default"].START_STREAMING, this._onMSEStartStreaming.bind(this));
        this._mse_controller.on(_core_mse_events__WEBPACK_IMPORTED_MODULE_6__["default"].END_STREAMING, this._onMSEEndStreaming.bind(this));
        this._mse_controller.initialize({
            getCurrentTime: function () { return _this._media_element.currentTime; },
            getReadyState: function () { return _this._media_element.readyState; },
        });
        // Attach media source into media element
        if (this._mse_controller.isManagedMediaSource()) {
            // Apple ManagedMediaSource
            mediaElement['disableRemotePlayback'] = true;
            mediaElement.srcObject = this._mse_controller.getObject();
        }
        else {
            // w3c MediaSource
            mediaElement.src = this._mse_controller.getObjectURL();
        }
    };
    PlayerEngineMainThread.prototype.detachMediaElement = function () {
        if (this._media_element) {
            this._mse_controller.shutdown();
            // Remove all appended event listeners
            this._media_element.removeEventListener('loadedmetadata', this.e.onMediaLoadedMetadata);
            // Detach media source from media element
            this._media_element.src = '';
            this._media_element.removeAttribute('src');
            this._media_element.srcObject = null;
            this._media_element.load();
            this._media_element = null;
            this._mse_controller.revokeObjectURL();
        }
        if (this._mse_controller) {
            this._mse_controller.destroy();
            this._mse_controller = null;
        }
    };
    PlayerEngineMainThread.prototype.load = function () {
        var _this = this;
        if (!this._media_element) {
            throw new _utils_exception__WEBPACK_IMPORTED_MODULE_8__["IllegalStateException"]('HTMLMediaElement must be attached before load()!');
        }
        if (this._transmuxer) {
            throw new _utils_exception__WEBPACK_IMPORTED_MODULE_8__["IllegalStateException"]('load() has been called, please call unload() first!');
        }
        if (this._has_pending_load) {
            // Defer load operation until MSE source open
            return;
        }
        if (this._config.deferLoadAfterSourceOpen && !this._mse_source_opened) {
            this._has_pending_load = true;
            return;
        }
        this._transmuxer = new _core_transmuxer__WEBPACK_IMPORTED_MODULE_5__["default"](this._media_data_source, this._config);
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].INIT_SEGMENT, function (type, is) {
            _this._mse_controller.appendInitSegment(is);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].MEDIA_SEGMENT, function (type, ms) {
            _this._mse_controller.appendMediaSegment(ms);
            if (!_this._config.isLive && type === 'video' && ms.data && ms.data.byteLength > 0 && ('info' in ms)) {
                _this._seeking_handler.appendSyncPoints(ms.info.syncPoints);
            }
            _this._loading_controller.notifyBufferedPositionChanged(ms.info.endDts / 1000);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].LOADING_COMPLETE, function () {
            _this._mse_controller.endOfStream();
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].LOADING_COMPLETE);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].RECOVERED_EARLY_EOF, function () {
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].RECOVERED_EARLY_EOF);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].IO_ERROR, function (detail, info) {
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].ERROR, _player_errors__WEBPACK_IMPORTED_MODULE_7__["ErrorTypes"].NETWORK_ERROR, detail, info);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].DEMUX_ERROR, function (detail, info) {
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].ERROR, _player_errors__WEBPACK_IMPORTED_MODULE_7__["ErrorTypes"].MEDIA_ERROR, detail, info);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].MEDIA_INFO, function (mediaInfo) {
            _this._media_info = mediaInfo;
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].MEDIA_INFO, Object.assign({}, mediaInfo));
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].STATISTICS_INFO, function (statInfo) {
            _this._statistics_info = _this._fillStatisticsInfo(statInfo);
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].STATISTICS_INFO, Object.assign({}, statInfo));
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].RECOMMEND_SEEKPOINT, function (milliseconds) {
            if (_this._media_element && !_this._config.accurateSeek) {
                _this._seeking_handler.directSeek(milliseconds / 1000);
            }
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].METADATA_ARRIVED, function (metadata) {
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].METADATA_ARRIVED, metadata);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].SCRIPTDATA_ARRIVED, function (data) {
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].SCRIPTDATA_ARRIVED, data);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].TIMED_ID3_METADATA_ARRIVED, function (timed_id3_metadata) {
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].TIMED_ID3_METADATA_ARRIVED, timed_id3_metadata);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].PGS_SUBTITLE_ARRIVED, function (pgs_data) {
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].PGS_SUBTITLE_ARRIVED, pgs_data);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].SYNCHRONOUS_KLV_METADATA_ARRIVED, function (synchronous_klv_metadata) {
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].SYNCHRONOUS_KLV_METADATA_ARRIVED, synchronous_klv_metadata);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].ASYNCHRONOUS_KLV_METADATA_ARRIVED, function (asynchronous_klv_metadata) {
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].ASYNCHRONOUS_KLV_METADATA_ARRIVED, asynchronous_klv_metadata);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].SMPTE2038_METADATA_ARRIVED, function (smpte2038_metadata) {
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].SMPTE2038_METADATA_ARRIVED, smpte2038_metadata);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].SCTE35_METADATA_ARRIVED, function (scte35_metadata) {
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].SCTE35_METADATA_ARRIVED, scte35_metadata);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].PES_PRIVATE_DATA_DESCRIPTOR, function (descriptor) {
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].PES_PRIVATE_DATA_DESCRIPTOR, descriptor);
        });
        this._transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_9__["default"].PES_PRIVATE_DATA_ARRIVED, function (private_data) {
            _this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].PES_PRIVATE_DATA_ARRIVED, private_data);
        });
        this._seeking_handler = new _seeking_handler__WEBPACK_IMPORTED_MODULE_10__["default"](this._config, this._media_element, this._onRequiredUnbufferedSeek.bind(this));
        this._loading_controller = new _loading_controller__WEBPACK_IMPORTED_MODULE_11__["default"](this._config, this._media_element, this._onRequestPauseTransmuxer.bind(this), this._onRequestResumeTransmuxer.bind(this));
        this._startup_stall_jumper = new _startup_stall_jumper__WEBPACK_IMPORTED_MODULE_12__["default"](this._media_element, this._onRequestDirectSeek.bind(this));
        if (this._config.isLive && this._config.liveBufferLatencyChasing) {
            this._live_latency_chaser = new _live_latency_chaser__WEBPACK_IMPORTED_MODULE_13__["default"](this._config, this._media_element, this._onRequestDirectSeek.bind(this));
        }
        if (this._config.isLive && this._config.liveSync) {
            this._live_latency_synchronizer = new _live_latency_synchronizer__WEBPACK_IMPORTED_MODULE_14__["default"](this._config, this._media_element);
        }
        // Reset currentTime to 0
        if (this._media_element.readyState > 0) {
            // IE11 may throw InvalidStateError if readyState === 0
            this._seeking_handler.directSeek(0);
        }
        this._transmuxer.open();
    };
    PlayerEngineMainThread.prototype.unload = function () {
        var _a, _b, _c, _d, _e, _f, _g, _h, _j;
        (_a = this._media_element) === null || _a === void 0 ? void 0 : _a.pause();
        (_b = this._live_latency_synchronizer) === null || _b === void 0 ? void 0 : _b.destroy();
        this._live_latency_synchronizer = null;
        (_c = this._live_latency_chaser) === null || _c === void 0 ? void 0 : _c.destroy();
        this._live_latency_chaser = null;
        (_d = this._startup_stall_jumper) === null || _d === void 0 ? void 0 : _d.destroy();
        this._startup_stall_jumper = null;
        (_e = this._loading_controller) === null || _e === void 0 ? void 0 : _e.destroy();
        this._loading_controller = null;
        (_f = this._seeking_handler) === null || _f === void 0 ? void 0 : _f.destroy();
        this._seeking_handler = null;
        (_g = this._mse_controller) === null || _g === void 0 ? void 0 : _g.flush();
        (_h = this._transmuxer) === null || _h === void 0 ? void 0 : _h.close();
        (_j = this._transmuxer) === null || _j === void 0 ? void 0 : _j.destroy();
        this._transmuxer = null;
    };
    PlayerEngineMainThread.prototype.play = function () {
        return this._media_element.play();
    };
    PlayerEngineMainThread.prototype.pause = function () {
        this._media_element.pause();
    };
    PlayerEngineMainThread.prototype.seek = function (seconds) {
        if (this._media_element && this._seeking_handler) {
            this._seeking_handler.seek(seconds);
        }
        else {
            this._pending_seek_time = seconds;
        }
    };
    Object.defineProperty(PlayerEngineMainThread.prototype, "mediaInfo", {
        get: function () {
            return Object.assign({}, this._media_info);
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(PlayerEngineMainThread.prototype, "statisticsInfo", {
        get: function () {
            return Object.assign({}, this._statistics_info);
        },
        enumerable: false,
        configurable: true
    });
    PlayerEngineMainThread.prototype._onMSESourceOpen = function () {
        this._mse_source_opened = true;
        if (this._has_pending_load) {
            this._has_pending_load = false;
            this.load();
        }
    };
    PlayerEngineMainThread.prototype._onMSEUpdateEnd = function () {
        if (this._config.isLive && this._config.liveBufferLatencyChasing && this._live_latency_chaser) {
            this._live_latency_chaser.notifyBufferedRangeUpdate();
        }
        this._loading_controller.notifyBufferedPositionChanged();
    };
    PlayerEngineMainThread.prototype._onMSEBufferFull = function () {
        _utils_logger__WEBPACK_IMPORTED_MODULE_1__["default"].v(this.TAG, 'MSE SourceBuffer is full, suspend transmuxing task');
        this._loading_controller.suspendTransmuxer();
    };
    PlayerEngineMainThread.prototype._onMSEError = function (info) {
        this._emitter.emit(_player_events__WEBPACK_IMPORTED_MODULE_4__["default"].ERROR, _player_errors__WEBPACK_IMPORTED_MODULE_7__["ErrorTypes"].MEDIA_ERROR, _player_errors__WEBPACK_IMPORTED_MODULE_7__["ErrorDetails"].MEDIA_MSE_ERROR, info);
    };
    PlayerEngineMainThread.prototype._onMSEStartStreaming = function () {
        if (!this._loaded_metadata_received) {
            // Ignore initial startstreaming event since we have started loading data
            return;
        }
        if (this._config.isLive) {
            // For live stream, we do not suspend / resume transmuxer
            return;
        }
        _utils_logger__WEBPACK_IMPORTED_MODULE_1__["default"].v(this.TAG, 'Resume transmuxing task due to ManagedMediaSource onStartStreaming');
        this._loading_controller.resumeTransmuxer();
    };
    PlayerEngineMainThread.prototype._onMSEEndStreaming = function () {
        if (this._config.isLive) {
            // For live stream, we do not suspend / resume transmuxer
            return;
        }
        _utils_logger__WEBPACK_IMPORTED_MODULE_1__["default"].v(this.TAG, 'Suspend transmuxing task due to ManagedMediaSource onEndStreaming');
        this._loading_controller.suspendTransmuxer();
    };
    PlayerEngineMainThread.prototype._onMediaLoadedMetadata = function (e) {
        this._loaded_metadata_received = true;
        if (this._pending_seek_time != null) {
            this._seeking_handler.seek(this._pending_seek_time);
            this._pending_seek_time = null;
        }
    };
    PlayerEngineMainThread.prototype._onRequestDirectSeek = function (target) {
        this._seeking_handler.directSeek(target);
    };
    PlayerEngineMainThread.prototype._onRequiredUnbufferedSeek = function (milliseconds) {
        this._mse_controller.flush();
        this._transmuxer.seek(milliseconds);
    };
    PlayerEngineMainThread.prototype._onRequestPauseTransmuxer = function () {
        this._transmuxer.pause();
    };
    PlayerEngineMainThread.prototype._onRequestResumeTransmuxer = function () {
        this._transmuxer.resume();
    };
    PlayerEngineMainThread.prototype._fillStatisticsInfo = function (stat_info) {
        stat_info.playerType = 'MSEPlayer';
        if (!(this._media_element instanceof HTMLVideoElement)) {
            return stat_info;
        }
        var has_quality_info = true;
        var decoded = 0;
        var dropped = 0;
        if (this._media_element.getVideoPlaybackQuality) {
            var quality = this._media_element.getVideoPlaybackQuality();
            decoded = quality.totalVideoFrames;
            dropped = quality.droppedVideoFrames;
        }
        else if (this._media_element['webkitDecodedFrameCount'] != undefined) {
            decoded = this._media_element['webkitDecodedFrameCount'];
            dropped = this._media_element['webkitDroppedFrameCount'];
        }
        else {
            has_quality_info = false;
        }
        if (has_quality_info) {
            stat_info.decodedFrames = decoded;
            stat_info.droppedFrames = dropped;
        }
        return stat_info;
    };
    return PlayerEngineMainThread;
}());
/* harmony default export */ __webpack_exports__["default"] = (PlayerEngineMainThread);


/***/ }),

/***/ "./src/player/player-engine-worker.ts":
/*!********************************************!*\
  !*** ./src/player/player-engine-worker.ts ***!
  \********************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger */ "./src/utils/logger.js");
/* harmony import */ var _utils_logging_control__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ../utils/logging-control */ "./src/utils/logging-control.js");
/* harmony import */ var _utils_exception__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ../utils/exception */ "./src/utils/exception.js");
/* harmony import */ var _core_mse_events__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ../core/mse-events */ "./src/core/mse-events.ts");
/* harmony import */ var _core_mse_controller__WEBPACK_IMPORTED_MODULE_4__ = __webpack_require__(/*! ../core/mse-controller */ "./src/core/mse-controller.js");
/* harmony import */ var _core_transmuxer__WEBPACK_IMPORTED_MODULE_5__ = __webpack_require__(/*! ../core/transmuxer */ "./src/core/transmuxer.js");
/* harmony import */ var _core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__ = __webpack_require__(/*! ../core/transmuxing-events */ "./src/core/transmuxing-events.ts");
/* harmony import */ var _player_events__WEBPACK_IMPORTED_MODULE_7__ = __webpack_require__(/*! ./player-events */ "./src/player/player-events.ts");
/* harmony import */ var _player_errors__WEBPACK_IMPORTED_MODULE_8__ = __webpack_require__(/*! ./player-errors */ "./src/player/player-errors.js");
/*
 * Copyright (C) 2023 zheng qian. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var _this = undefined;









var PlayerEngineWorker = function (self) {
    var TAG = 'PlayerEngineWorker';
    var logcat_callback = onLogcatCallback.bind(_this);
    var media_data_source = null;
    var config = null;
    var mse_controller = null;
    var transmuxer = null;
    var mse_source_opened = false;
    var has_pending_load = false;
    var media_element_current_time = 0;
    var media_element_ready_state = 0;
    var destroyed = false;
    self.addEventListener('message', function (e) {
        if (destroyed) {
            return;
        }
        var command_packet = e.data;
        var cmd = command_packet.cmd;
        switch (cmd) {
            case 'logging_config': {
                var packet = command_packet;
                _utils_logging_control__WEBPACK_IMPORTED_MODULE_1__["default"].applyConfig(packet.logging_config);
                if (packet.logging_config.enableCallback === true) {
                    _utils_logging_control__WEBPACK_IMPORTED_MODULE_1__["default"].addLogListener(logcat_callback);
                }
                else {
                    _utils_logging_control__WEBPACK_IMPORTED_MODULE_1__["default"].removeLogListener(logcat_callback);
                }
                break;
            }
            case 'init': {
                var packet = command_packet;
                media_data_source = packet.media_data_source;
                config = packet.config;
                break;
            }
            case 'destroy':
                destroy();
                break;
            case 'initialize_mse':
                initializeMSE();
                break;
            case 'shutdown_mse':
                shutdownMSE();
                break;
            case 'load':
                load();
                break;
            case 'unload':
                unload();
                break;
            case 'unbuffered_seek': {
                var packet = command_packet;
                mse_controller.flush();
                transmuxer.seek(packet.milliseconds);
                break;
            }
            case 'timeupdate': {
                var packet = command_packet;
                media_element_current_time = packet.current_time;
                break;
            }
            case 'readystatechange': {
                var packet = command_packet;
                media_element_ready_state = packet.ready_state;
                break;
            }
            case 'pause_transmuxer':
                transmuxer.pause();
                break;
            case 'resume_transmuxer':
                transmuxer.resume();
                break;
        }
    });
    function destroy() {
        if (transmuxer) {
            unload();
        }
        if (mse_controller) {
            shutdownMSE();
        }
        destroyed = true;
        self.postMessage({
            msg: 'destroyed',
        });
    }
    function initializeMSE() {
        _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(TAG, 'Initializing MediaSource in DedicatedWorker');
        mse_controller = new _core_mse_controller__WEBPACK_IMPORTED_MODULE_4__["default"](config);
        mse_controller.on(_core_mse_events__WEBPACK_IMPORTED_MODULE_3__["default"].SOURCE_OPEN, onMSESourceOpen.bind(this));
        mse_controller.on(_core_mse_events__WEBPACK_IMPORTED_MODULE_3__["default"].UPDATE_END, onMSEUpdateEnd.bind(this));
        mse_controller.on(_core_mse_events__WEBPACK_IMPORTED_MODULE_3__["default"].BUFFER_FULL, onMSEBufferFull.bind(this));
        mse_controller.on(_core_mse_events__WEBPACK_IMPORTED_MODULE_3__["default"].ERROR, onMSEError.bind(this));
        mse_controller.initialize({
            getCurrentTime: function () { return media_element_current_time; },
            getReadyState: function () { return media_element_ready_state; },
        });
        var handle = mse_controller.getHandle();
        self.postMessage({
            msg: 'mse_init',
            handle: handle,
        }, [handle]);
    }
    function shutdownMSE() {
        if (mse_controller) {
            mse_controller.shutdown();
            mse_controller.destroy();
            mse_controller = null;
        }
    }
    function load() {
        if (media_data_source == null || config == null) {
            throw new _utils_exception__WEBPACK_IMPORTED_MODULE_2__["IllegalStateException"]('Worker not initialized');
        }
        if (transmuxer) {
            throw new _utils_exception__WEBPACK_IMPORTED_MODULE_2__["IllegalStateException"]('Transmuxer has been initialized');
        }
        if (has_pending_load) {
            return;
        }
        if (config.deferLoadAfterSourceOpen && !mse_source_opened) {
            has_pending_load = true;
            return;
        }
        transmuxer = new _core_transmuxer__WEBPACK_IMPORTED_MODULE_5__["default"](media_data_source, config);
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].INIT_SEGMENT, function (type, is) {
            mse_controller.appendInitSegment(is);
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].MEDIA_SEGMENT, function (type, ms) {
            mse_controller.appendMediaSegment(ms);
            self.postMessage({
                msg: 'buffered_position_changed',
                buffered_position_milliseconds: ms.info.endDts,
            });
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].LOADING_COMPLETE, function () {
            mse_controller.endOfStream();
            self.postMessage({
                msg: 'player_event',
                event: _player_events__WEBPACK_IMPORTED_MODULE_7__["default"].LOADING_COMPLETE,
            });
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].RECOVERED_EARLY_EOF, function () {
            self.postMessage({
                msg: 'player_event',
                event: _player_events__WEBPACK_IMPORTED_MODULE_7__["default"].RECOVERED_EARLY_EOF,
            });
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].IO_ERROR, function (detail, info) {
            self.postMessage({
                msg: 'player_event',
                event: _player_events__WEBPACK_IMPORTED_MODULE_7__["default"].ERROR,
                error_type: _player_errors__WEBPACK_IMPORTED_MODULE_8__["ErrorTypes"].NETWORK_ERROR,
                error_detail: detail,
                info: info,
            });
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].DEMUX_ERROR, function (detail, info) {
            self.postMessage({
                msg: 'player_event',
                event: _player_events__WEBPACK_IMPORTED_MODULE_7__["default"].ERROR,
                error_type: _player_errors__WEBPACK_IMPORTED_MODULE_8__["ErrorTypes"].MEDIA_ERROR,
                error_detail: detail,
                info: info,
            });
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].MEDIA_INFO, function (mediaInfo) {
            emitTransmuxingEventsInfo(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].MEDIA_INFO, mediaInfo);
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].STATISTICS_INFO, function (statInfo) {
            emitTransmuxingEventsInfo(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].STATISTICS_INFO, statInfo);
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].RECOMMEND_SEEKPOINT, function (milliseconds) {
            emitTransmuxingEventsRecommendSeekpoint(milliseconds);
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].METADATA_ARRIVED, function (metadata) {
            emitPlayerEventsExtraData(_player_events__WEBPACK_IMPORTED_MODULE_7__["default"].METADATA_ARRIVED, metadata);
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].SCRIPTDATA_ARRIVED, function (data) {
            emitPlayerEventsExtraData(_player_events__WEBPACK_IMPORTED_MODULE_7__["default"].SCRIPTDATA_ARRIVED, data);
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].TIMED_ID3_METADATA_ARRIVED, function (timed_id3_metadata) {
            emitPlayerEventsExtraData(_player_events__WEBPACK_IMPORTED_MODULE_7__["default"].TIMED_ID3_METADATA_ARRIVED, timed_id3_metadata);
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].PGS_SUBTITLE_ARRIVED, function (pgs_data) {
            emitPlayerEventsExtraData(_player_events__WEBPACK_IMPORTED_MODULE_7__["default"].PGS_SUBTITLE_ARRIVED, pgs_data);
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].SYNCHRONOUS_KLV_METADATA_ARRIVED, function (synchronous_klv_metadata) {
            emitPlayerEventsExtraData(_player_events__WEBPACK_IMPORTED_MODULE_7__["default"].SYNCHRONOUS_KLV_METADATA_ARRIVED, synchronous_klv_metadata);
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].ASYNCHRONOUS_KLV_METADATA_ARRIVED, function (asynchronous_klv_metadata) {
            emitPlayerEventsExtraData(_player_events__WEBPACK_IMPORTED_MODULE_7__["default"].ASYNCHRONOUS_KLV_METADATA_ARRIVED, asynchronous_klv_metadata);
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].SMPTE2038_METADATA_ARRIVED, function (smpte2038_metadata) {
            emitPlayerEventsExtraData(_player_events__WEBPACK_IMPORTED_MODULE_7__["default"].SMPTE2038_METADATA_ARRIVED, smpte2038_metadata);
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].SCTE35_METADATA_ARRIVED, function (scte35_metadata) {
            emitPlayerEventsExtraData(_player_events__WEBPACK_IMPORTED_MODULE_7__["default"].SCTE35_METADATA_ARRIVED, scte35_metadata);
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].PES_PRIVATE_DATA_DESCRIPTOR, function (descriptor) {
            emitPlayerEventsExtraData(_player_events__WEBPACK_IMPORTED_MODULE_7__["default"].PES_PRIVATE_DATA_DESCRIPTOR, descriptor);
        });
        transmuxer.on(_core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].PES_PRIVATE_DATA_ARRIVED, function (private_data) {
            emitPlayerEventsExtraData(_player_events__WEBPACK_IMPORTED_MODULE_7__["default"].PES_PRIVATE_DATA_ARRIVED, private_data);
        });
        transmuxer.open();
    }
    function unload() {
        if (mse_controller) {
            mse_controller.flush();
        }
        if (transmuxer) {
            transmuxer.close();
            transmuxer.destroy();
            transmuxer = null;
        }
    }
    function onMSESourceOpen() {
        mse_source_opened = true;
        if (has_pending_load) {
            has_pending_load = false;
            load();
        }
    }
    function onMSEUpdateEnd() {
        self.postMessage({
            msg: 'mse_event',
            event: _core_mse_events__WEBPACK_IMPORTED_MODULE_3__["default"].UPDATE_END,
        });
    }
    function onMSEBufferFull() {
        _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].v(TAG, 'MSE SourceBuffer is full, report to main thread');
        self.postMessage({
            msg: 'mse_event',
            event: _core_mse_events__WEBPACK_IMPORTED_MODULE_3__["default"].BUFFER_FULL,
        });
    }
    function onMSEError(info) {
        self.postMessage({
            msg: 'player_event',
            event: _player_events__WEBPACK_IMPORTED_MODULE_7__["default"].ERROR,
            error_type: _player_errors__WEBPACK_IMPORTED_MODULE_8__["ErrorTypes"].MEDIA_ERROR,
            error_detail: _player_errors__WEBPACK_IMPORTED_MODULE_8__["ErrorTypes"].MEDIA_MSE_ERROR,
            info: info,
        });
    }
    function emitTransmuxingEventsRecommendSeekpoint(milliseconds) {
        self.postMessage({
            msg: 'transmuxing_event',
            event: _core_transmuxing_events__WEBPACK_IMPORTED_MODULE_6__["default"].RECOMMEND_SEEKPOINT,
            milliseconds: milliseconds,
        });
    }
    function emitTransmuxingEventsInfo(event, info) {
        self.postMessage({
            msg: 'transmuxing_event',
            event: event,
            info: info,
        });
    }
    function emitPlayerEventsExtraData(event, extraData) {
        self.postMessage({
            msg: 'player_event',
            event: event,
            extraData: extraData,
        });
    }
    function onLogcatCallback(type, str) {
        self.postMessage({
            msg: 'logcat_callback',
            type: type,
            logcat: str,
        });
    }
};
/* harmony default export */ __webpack_exports__["default"] = (PlayerEngineWorker);


/***/ }),

/***/ "./src/player/player-errors.js":
/*!*************************************!*\
  !*** ./src/player/player-errors.js ***!
  \*************************************/
/*! exports provided: ErrorTypes, ErrorDetails */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "ErrorTypes", function() { return ErrorTypes; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "ErrorDetails", function() { return ErrorDetails; });
/* harmony import */ var _io_loader_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../io/loader.js */ "./src/io/loader.js");
/* harmony import */ var _demux_demux_errors_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ../demux/demux-errors.js */ "./src/demux/demux-errors.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */


var ErrorTypes = {
    NETWORK_ERROR: 'NetworkError',
    MEDIA_ERROR: 'MediaError',
    OTHER_ERROR: 'OtherError'
};
var ErrorDetails = {
    NETWORK_EXCEPTION: _io_loader_js__WEBPACK_IMPORTED_MODULE_0__["LoaderErrors"].EXCEPTION,
    NETWORK_STATUS_CODE_INVALID: _io_loader_js__WEBPACK_IMPORTED_MODULE_0__["LoaderErrors"].HTTP_STATUS_CODE_INVALID,
    NETWORK_TIMEOUT: _io_loader_js__WEBPACK_IMPORTED_MODULE_0__["LoaderErrors"].CONNECTING_TIMEOUT,
    NETWORK_UNRECOVERABLE_EARLY_EOF: _io_loader_js__WEBPACK_IMPORTED_MODULE_0__["LoaderErrors"].UNRECOVERABLE_EARLY_EOF,
    MEDIA_MSE_ERROR: 'MediaMSEError',
    MEDIA_FORMAT_ERROR: _demux_demux_errors_js__WEBPACK_IMPORTED_MODULE_1__["default"].FORMAT_ERROR,
    MEDIA_FORMAT_UNSUPPORTED: _demux_demux_errors_js__WEBPACK_IMPORTED_MODULE_1__["default"].FORMAT_UNSUPPORTED,
    MEDIA_CODEC_UNSUPPORTED: _demux_demux_errors_js__WEBPACK_IMPORTED_MODULE_1__["default"].CODEC_UNSUPPORTED
};


/***/ }),

/***/ "./src/player/player-events.ts":
/*!*************************************!*\
  !*** ./src/player/player-events.ts ***!
  \*************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var PlayerEvents;
(function (PlayerEvents) {
    PlayerEvents["ERROR"] = "error";
    PlayerEvents["LOADING_COMPLETE"] = "loading_complete";
    PlayerEvents["RECOVERED_EARLY_EOF"] = "recovered_early_eof";
    PlayerEvents["MEDIA_INFO"] = "media_info";
    PlayerEvents["METADATA_ARRIVED"] = "metadata_arrived";
    PlayerEvents["SCRIPTDATA_ARRIVED"] = "scriptdata_arrived";
    PlayerEvents["TIMED_ID3_METADATA_ARRIVED"] = "timed_id3_metadata_arrived";
    PlayerEvents["PGS_SUBTITLE_ARRIVED"] = "pgs_subtitle_arrived";
    PlayerEvents["SYNCHRONOUS_KLV_METADATA_ARRIVED"] = "synchronous_klv_metadata_arrived";
    PlayerEvents["ASYNCHRONOUS_KLV_METADATA_ARRIVED"] = "asynchronous_klv_metadata_arrived";
    PlayerEvents["SMPTE2038_METADATA_ARRIVED"] = "smpte2038_metadata_arrived";
    PlayerEvents["SCTE35_METADATA_ARRIVED"] = "scte35_metadata_arrived";
    PlayerEvents["PES_PRIVATE_DATA_DESCRIPTOR"] = "pes_private_data_descriptor";
    PlayerEvents["PES_PRIVATE_DATA_ARRIVED"] = "pes_private_data_arrived";
    PlayerEvents["STATISTICS_INFO"] = "statistics_info";
    PlayerEvents["DESTROYING"] = "destroying";
})(PlayerEvents || (PlayerEvents = {}));
;
/* harmony default export */ __webpack_exports__["default"] = (PlayerEvents);


/***/ }),

/***/ "./src/player/seeking-handler.ts":
/*!***************************************!*\
  !*** ./src/player/seeking-handler.ts ***!
  \***************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_browser__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/browser */ "./src/utils/browser.js");
/* harmony import */ var _core_media_segment_info__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ../core/media-segment-info */ "./src/core/media-segment-info.js");
/*
 * Copyright (C) 2023 zheng qian. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */


var SeekingHandler = /** @class */ (function () {
    function SeekingHandler(config, media_element, on_unbuffered_seek) {
        this.TAG = 'SeekingHandler';
        this._config = null;
        this._media_element = null;
        this._always_seek_keyframe = false;
        this._on_unbuffered_seek = null;
        this._request_set_current_time = false;
        this._seek_request_record_clocktime = null;
        this._idr_sample_list = new _core_media_segment_info__WEBPACK_IMPORTED_MODULE_1__["IDRSampleList"]();
        this.e = null;
        this._config = config;
        this._media_element = media_element;
        this._on_unbuffered_seek = on_unbuffered_seek;
        this.e = {
            onMediaSeeking: this._onMediaSeeking.bind(this),
        };
        var chrome_need_idr_fix = (_utils_browser__WEBPACK_IMPORTED_MODULE_0__["default"].chrome &&
            (_utils_browser__WEBPACK_IMPORTED_MODULE_0__["default"].version.major < 50 ||
                (_utils_browser__WEBPACK_IMPORTED_MODULE_0__["default"].version.major === 50 && _utils_browser__WEBPACK_IMPORTED_MODULE_0__["default"].version.build < 2661)));
        this._always_seek_keyframe = (chrome_need_idr_fix || _utils_browser__WEBPACK_IMPORTED_MODULE_0__["default"].msedge || _utils_browser__WEBPACK_IMPORTED_MODULE_0__["default"].msie) ? true : false;
        if (this._always_seek_keyframe) {
            this._config.accurateSeek = false;
        }
        this._media_element.addEventListener('seeking', this.e.onMediaSeeking);
    }
    SeekingHandler.prototype.destroy = function () {
        this._idr_sample_list.clear();
        this._idr_sample_list = null;
        this._media_element.removeEventListener('seeking', this.e.onMediaSeeking);
        this._media_element = null;
        this._on_unbuffered_seek = null;
    };
    SeekingHandler.prototype.seek = function (seconds) {
        var direct_seek = this._isPositionBuffered(seconds);
        var direct_seek_to_video_begin = false;
        if (seconds < 1.0 && this._media_element.buffered.length > 0) {
            var video_begin_time = this._media_element.buffered.start(0);
            if ((video_begin_time < 1.0 && seconds < video_begin_time) || _utils_browser__WEBPACK_IMPORTED_MODULE_0__["default"].safari) {
                direct_seek_to_video_begin = true;
                // Workaround for Safari: Seek to 0 may cause video stuck, use 0.1 to avoid
                seconds = _utils_browser__WEBPACK_IMPORTED_MODULE_0__["default"].safari ? 0.1 : video_begin_time;
            }
        }
        if (direct_seek_to_video_begin) {
            this.directSeek(seconds);
        }
        else if (direct_seek) {
            if (!this._always_seek_keyframe) {
                this.directSeek(seconds);
            }
            else {
                // For some old browsers we have to seek to keyframe
                // Seek to nearest keyframe if possible
                var idr = this._getNearestKeyframe(Math.floor(seconds * 1000));
                if (idr != null) {
                    seconds = idr.dts / 1000;
                }
                this.directSeek(seconds);
            }
        }
        else {
            this._idr_sample_list.clear();
            this._on_unbuffered_seek(Math.floor(seconds * 1000)); // In milliseconds
            if (this._config.accurateSeek) {
                this.directSeek(seconds);
            }
            // else: Wait for recommend_seekpoint callback
        }
    };
    SeekingHandler.prototype.directSeek = function (seconds) {
        this._request_set_current_time = true;
        this._media_element.currentTime = seconds;
    };
    SeekingHandler.prototype.appendSyncPoints = function (syncpoints) {
        this._idr_sample_list.appendArray(syncpoints);
    };
    // Handle seeking request from browser's progress bar or HTMLMediaElement.currentTime setter
    SeekingHandler.prototype._onMediaSeeking = function (e) {
        if (this._request_set_current_time) {
            this._request_set_current_time = false;
            return;
        }
        var target = this._media_element.currentTime;
        var buffered = this._media_element.buffered;
        // Handle seeking to video begin (near 0.0s)
        if (target < 1.0 && buffered.length > 0) {
            var video_begin_time = buffered.start(0);
            if ((video_begin_time < 1.0 && target < video_begin_time) || _utils_browser__WEBPACK_IMPORTED_MODULE_0__["default"].safari) {
                // Safari may get stuck if currentTime set to 0, use 0.1 to avoid
                var target_1 = _utils_browser__WEBPACK_IMPORTED_MODULE_0__["default"].safari ? 0.1 : video_begin_time;
                this.directSeek(target_1);
                return;
            }
        }
        // Handle in-buffer seeking (usually nothing to do)
        if (this._isPositionBuffered(target)) {
            if (this._always_seek_keyframe) {
                var idr = this._getNearestKeyframe(Math.floor(target * 1000));
                if (idr != null) {
                    target = idr.dts / 1000;
                    this.directSeek(target);
                }
            }
            return;
        }
        // else: Prepare for unbuffered seeking
        // Defer the unbuffered seeking since the seeking bar maybe still being draged
        this._seek_request_record_clocktime = SeekingHandler._getClockTime();
        window.setTimeout(this._pollAndApplyUnbufferedSeek.bind(this), 50);
    };
    SeekingHandler.prototype._pollAndApplyUnbufferedSeek = function () {
        if (this._seek_request_record_clocktime == null) {
            return;
        }
        var record_time = this._seek_request_record_clocktime;
        if (record_time <= SeekingHandler._getClockTime() - 100) {
            var target = this._media_element.currentTime;
            this._seek_request_record_clocktime = null;
            if (!this._isPositionBuffered(target)) {
                this._idr_sample_list.clear();
                this._on_unbuffered_seek(Math.floor(target * 1000)); // In milliseconds
                // Update currentTime if using accurateSeek, or wait for recommend_seekpoint callback
                if (this._config.accurateSeek) {
                    this.directSeek(target);
                }
            }
        }
        else {
            window.setTimeout(this._pollAndApplyUnbufferedSeek.bind(this), 50);
        }
    };
    SeekingHandler.prototype._isPositionBuffered = function (seconds) {
        var buffered = this._media_element.buffered;
        for (var i = 0; i < buffered.length; i++) {
            var from = buffered.start(i);
            var to = buffered.end(i);
            if (seconds >= from && seconds < to) {
                return true;
            }
        }
        return false;
    };
    SeekingHandler.prototype._getNearestKeyframe = function (dts) {
        return this._idr_sample_list.getLastSyncPointBeforeDts(dts);
    };
    SeekingHandler._getClockTime = function () {
        if (self.performance && self.performance.now) {
            return self.performance.now();
        }
        else {
            return Date.now();
        }
    };
    return SeekingHandler;
}());
/* harmony default export */ __webpack_exports__["default"] = (SeekingHandler);


/***/ }),

/***/ "./src/player/startup-stall-jumper.ts":
/*!********************************************!*\
  !*** ./src/player/startup-stall-jumper.ts ***!
  \********************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger */ "./src/utils/logger.js");
/*
 * Copyright (C) 2023 zheng qian. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

var StartupStallJumper = /** @class */ (function () {
    function StartupStallJumper(media_element, on_direct_seek) {
        this.TAG = 'StartupStallJumper';
        this._media_element = null;
        this._on_direct_seek = null;
        this._canplay_received = false;
        this.e = null;
        this._media_element = media_element;
        this._on_direct_seek = on_direct_seek;
        this.e = {
            onMediaCanPlay: this._onMediaCanPlay.bind(this),
            onMediaStalled: this._onMediaStalled.bind(this),
            onMediaProgress: this._onMediaProgress.bind(this),
        };
        this._media_element.addEventListener('canplay', this.e.onMediaCanPlay);
        this._media_element.addEventListener('stalled', this.e.onMediaStalled);
        this._media_element.addEventListener('progress', this.e.onMediaProgress);
    }
    StartupStallJumper.prototype.destroy = function () {
        this._media_element.removeEventListener('canplay', this.e.onMediaCanPlay);
        this._media_element.removeEventListener('stalled', this.e.onMediaStalled);
        this._media_element.removeEventListener('progress', this.e.onMediaProgress);
        this._media_element = null;
        this._on_direct_seek = null;
    };
    StartupStallJumper.prototype._onMediaCanPlay = function (e) {
        this._canplay_received = true;
        // Remove canplay listener since it will be fired multiple times
        this._media_element.removeEventListener('canplay', this.e.onMediaCanPlay);
    };
    StartupStallJumper.prototype._onMediaStalled = function (e) {
        this._detectAndFixStuckPlayback(true);
    };
    StartupStallJumper.prototype._onMediaProgress = function (e) {
        this._detectAndFixStuckPlayback();
    };
    StartupStallJumper.prototype._detectAndFixStuckPlayback = function (is_stalled) {
        var media = this._media_element;
        var buffered = media.buffered;
        if (is_stalled || !this._canplay_received || media.readyState < 2) { // HAVE_CURRENT_DATA
            if (buffered.length > 0 && media.currentTime < buffered.start(0)) {
                _utils_logger__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "Playback seems stuck at ".concat(media.currentTime, ", seek to ").concat(buffered.start(0)));
                this._on_direct_seek(buffered.start(0));
                this._media_element.removeEventListener('progress', this.e.onMediaProgress);
            }
        }
        else {
            // Playback doesn't stuck, remove progress event listener
            this._media_element.removeEventListener('progress', this.e.onMediaProgress);
        }
    };
    return StartupStallJumper;
}());
/* harmony default export */ __webpack_exports__["default"] = (StartupStallJumper);


/***/ }),

/***/ "./src/remux/aac-silent.js":
/*!*********************************!*\
  !*** ./src/remux/aac-silent.js ***!
  \*********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * This file is modified from dailymotion's hls.js library (hls.js/src/helper/aac.js)
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var AAC = /** @class */ (function () {
    function AAC() {
    }
    AAC.getSilentFrame = function (codec, channelCount) {
        if (codec === 'mp4a.40.2') {
            // handle LC-AAC
            if (channelCount === 1) {
                return new Uint8Array([0x00, 0xc8, 0x00, 0x80, 0x23, 0x80]);
            }
            else if (channelCount === 2) {
                return new Uint8Array([0x21, 0x00, 0x49, 0x90, 0x02, 0x19, 0x00, 0x23, 0x80]);
            }
            else if (channelCount === 3) {
                return new Uint8Array([0x00, 0xc8, 0x00, 0x80, 0x20, 0x84, 0x01, 0x26, 0x40, 0x08, 0x64, 0x00, 0x8e]);
            }
            else if (channelCount === 4) {
                return new Uint8Array([0x00, 0xc8, 0x00, 0x80, 0x20, 0x84, 0x01, 0x26, 0x40, 0x08, 0x64, 0x00, 0x80, 0x2c, 0x80, 0x08, 0x02, 0x38]);
            }
            else if (channelCount === 5) {
                return new Uint8Array([0x00, 0xc8, 0x00, 0x80, 0x20, 0x84, 0x01, 0x26, 0x40, 0x08, 0x64, 0x00, 0x82, 0x30, 0x04, 0x99, 0x00, 0x21, 0x90, 0x02, 0x38]);
            }
            else if (channelCount === 6) {
                return new Uint8Array([0x00, 0xc8, 0x00, 0x80, 0x20, 0x84, 0x01, 0x26, 0x40, 0x08, 0x64, 0x00, 0x82, 0x30, 0x04, 0x99, 0x00, 0x21, 0x90, 0x02, 0x00, 0xb2, 0x00, 0x20, 0x08, 0xe0]);
            }
        }
        else {
            // handle HE-AAC (mp4a.40.5 / mp4a.40.29)
            if (channelCount === 1) {
                // ffmpeg -y -f lavfi -i "aevalsrc=0:d=0.05" -c:a libfdk_aac -profile:a aac_he -b:a 4k output.aac && hexdump -v -e '16/1 "0x%x," "\n"' -v output.aac
                return new Uint8Array([0x1, 0x40, 0x22, 0x80, 0xa3, 0x4e, 0xe6, 0x80, 0xba, 0x8, 0x0, 0x0, 0x0, 0x1c, 0x6, 0xf1, 0xc1, 0xa, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5e]);
            }
            else if (channelCount === 2) {
                // ffmpeg -y -f lavfi -i "aevalsrc=0|0:d=0.05" -c:a libfdk_aac -profile:a aac_he_v2 -b:a 4k output.aac && hexdump -v -e '16/1 "0x%x," "\n"' -v output.aac
                return new Uint8Array([0x1, 0x40, 0x22, 0x80, 0xa3, 0x5e, 0xe6, 0x80, 0xba, 0x8, 0x0, 0x0, 0x0, 0x0, 0x95, 0x0, 0x6, 0xf1, 0xa1, 0xa, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5e]);
            }
            else if (channelCount === 3) {
                // ffmpeg -y -f lavfi -i "aevalsrc=0|0|0:d=0.05" -c:a libfdk_aac -profile:a aac_he_v2 -b:a 4k output.aac && hexdump -v -e '16/1 "0x%x," "\n"' -v output.aac
                return new Uint8Array([0x1, 0x40, 0x22, 0x80, 0xa3, 0x5e, 0xe6, 0x80, 0xba, 0x8, 0x0, 0x0, 0x0, 0x0, 0x95, 0x0, 0x6, 0xf1, 0xa1, 0xa, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5a, 0x5e]);
            }
        }
        return null;
    };
    return AAC;
}());
/* harmony default export */ __webpack_exports__["default"] = (AAC);


/***/ }),

/***/ "./src/remux/mp4-generator.js":
/*!************************************!*\
  !*** ./src/remux/mp4-generator.js ***!
  \************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * This file is derived from dailymotion's hls.js library (hls.js/src/remux/mp4-generator.js)
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var __spreadArray = (undefined && undefined.__spreadArray) || function (to, from, pack) {
    if (pack || arguments.length === 2) for (var i = 0, l = from.length, ar; i < l; i++) {
        if (ar || !(i in from)) {
            if (!ar) ar = Array.prototype.slice.call(from, 0, i);
            ar[i] = from[i];
        }
    }
    return to.concat(ar || Array.prototype.slice.call(from));
};
//  MP4 boxes generator for ISO BMFF (ISO Base Media File Format, defined in ISO/IEC 14496-12)
var MP4 = /** @class */ (function () {
    function MP4() {
    }
    MP4.init = function () {
        MP4.types = {
            avc1: [], avcC: [], btrt: [], dinf: [],
            dref: [], esds: [], ftyp: [], hdlr: [],
            hvc1: [], hvcC: [], av01: [], av1C: [],
            mdat: [], mdhd: [], mdia: [], mfhd: [],
            minf: [], moof: [], moov: [], mp4a: [],
            mvex: [], mvhd: [], sdtp: [], stbl: [],
            stco: [], stsc: [], stsd: [], stsz: [],
            stts: [], tfdt: [], tfhd: [], traf: [],
            trak: [], trun: [], trex: [], tkhd: [],
            vmhd: [], smhd: [], chnl: [],
            '.mp3': [],
            Opus: [], dOps: [], fLaC: [], dfLa: [],
            ipcm: [], pcmC: [],
            'ac-3': [], dac3: [], 'ec-3': [], dec3: [],
        };
        for (var name_1 in MP4.types) {
            if (MP4.types.hasOwnProperty(name_1)) {
                MP4.types[name_1] = [
                    name_1.charCodeAt(0),
                    name_1.charCodeAt(1),
                    name_1.charCodeAt(2),
                    name_1.charCodeAt(3)
                ];
            }
        }
        var constants = MP4.constants = {};
        constants.FTYP = new Uint8Array([
            0x69, 0x73, 0x6F, 0x6D,
            0x0, 0x0, 0x0, 0x1,
            0x69, 0x73, 0x6F, 0x6D,
            0x61, 0x76, 0x63, 0x31 // avc1
        ]);
        constants.STSD_PREFIX = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x01 // entry_count
        ]);
        constants.STTS = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00 // entry_count
        ]);
        constants.STSC = constants.STCO = constants.STTS;
        constants.STSZ = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00 // sample_count
        ]);
        constants.HDLR_VIDEO = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x76, 0x69, 0x64, 0x65,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x56, 0x69, 0x64, 0x65,
            0x6F, 0x48, 0x61, 0x6E,
            0x64, 0x6C, 0x65, 0x72, 0x00 // name: VideoHandler
        ]);
        constants.HDLR_AUDIO = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x73, 0x6F, 0x75, 0x6E,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x53, 0x6F, 0x75, 0x6E,
            0x64, 0x48, 0x61, 0x6E,
            0x64, 0x6C, 0x65, 0x72, 0x00 // name: SoundHandler
        ]);
        constants.DREF = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x01,
            0x00, 0x00, 0x00, 0x0C,
            0x75, 0x72, 0x6C, 0x20,
            0x00, 0x00, 0x00, 0x01 // version(0) + flags
        ]);
        // Sound media header
        constants.SMHD = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00 // balance(2) + reserved(2)
        ]);
        // video media header
        constants.VMHD = new Uint8Array([
            0x00, 0x00, 0x00, 0x01,
            0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00
        ]);
    };
    // Generate a box
    MP4.box = function (type) {
        var size = 8;
        var result = null;
        var datas = Array.prototype.slice.call(arguments, 1);
        var arrayCount = datas.length;
        for (var i = 0; i < arrayCount; i++) {
            size += datas[i].byteLength;
        }
        result = new Uint8Array(size);
        result[0] = (size >>> 24) & 0xFF; // size
        result[1] = (size >>> 16) & 0xFF;
        result[2] = (size >>> 8) & 0xFF;
        result[3] = (size) & 0xFF;
        result.set(type, 4); // type
        var offset = 8;
        for (var i = 0; i < arrayCount; i++) { // data body
            result.set(datas[i], offset);
            offset += datas[i].byteLength;
        }
        return result;
    };
    // emit ftyp & moov
    MP4.generateInitSegment = function (meta) {
        var ftyp = MP4.box(MP4.types.ftyp, MP4.constants.FTYP);
        var moov = MP4.moov(meta);
        var result = new Uint8Array(ftyp.byteLength + moov.byteLength);
        result.set(ftyp, 0);
        result.set(moov, ftyp.byteLength);
        return result;
    };
    // Movie metadata box
    MP4.moov = function (meta) {
        var mvhd = MP4.mvhd(meta.timescale, meta.duration);
        var trak = MP4.trak(meta);
        var mvex = MP4.mvex(meta);
        return MP4.box(MP4.types.moov, mvhd, trak, mvex);
    };
    // Movie header box
    MP4.mvhd = function (timescale, duration) {
        return MP4.box(MP4.types.mvhd, new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            (timescale >>> 24) & 0xFF,
            (timescale >>> 16) & 0xFF,
            (timescale >>> 8) & 0xFF,
            (timescale) & 0xFF,
            (duration >>> 24) & 0xFF,
            (duration >>> 16) & 0xFF,
            (duration >>> 8) & 0xFF,
            (duration) & 0xFF,
            0x00, 0x01, 0x00, 0x00,
            0x01, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x01, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x01, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x40, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0xFF, 0xFF, 0xFF, 0xFF // next_track_ID
        ]));
    };
    // Track box
    MP4.trak = function (meta) {
        return MP4.box(MP4.types.trak, MP4.tkhd(meta), MP4.mdia(meta));
    };
    // Track header box
    MP4.tkhd = function (meta) {
        var trackId = meta.id, duration = meta.duration;
        var width = meta.presentWidth, height = meta.presentHeight;
        return MP4.box(MP4.types.tkhd, new Uint8Array([
            0x00, 0x00, 0x00, 0x07,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            (trackId >>> 24) & 0xFF,
            (trackId >>> 16) & 0xFF,
            (trackId >>> 8) & 0xFF,
            (trackId) & 0xFF,
            0x00, 0x00, 0x00, 0x00,
            (duration >>> 24) & 0xFF,
            (duration >>> 16) & 0xFF,
            (duration >>> 8) & 0xFF,
            (duration) & 0xFF,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x01, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x01, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x40, 0x00, 0x00, 0x00,
            (width >>> 8) & 0xFF,
            (width) & 0xFF,
            0x00, 0x00,
            (height >>> 8) & 0xFF,
            (height) & 0xFF,
            0x00, 0x00
        ]));
    };
    // Media Box
    MP4.mdia = function (meta) {
        return MP4.box(MP4.types.mdia, MP4.mdhd(meta), MP4.hdlr(meta), MP4.minf(meta));
    };
    // Media header box
    MP4.mdhd = function (meta) {
        var timescale = meta.timescale;
        var duration = meta.duration;
        return MP4.box(MP4.types.mdhd, new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            (timescale >>> 24) & 0xFF,
            (timescale >>> 16) & 0xFF,
            (timescale >>> 8) & 0xFF,
            (timescale) & 0xFF,
            (duration >>> 24) & 0xFF,
            (duration >>> 16) & 0xFF,
            (duration >>> 8) & 0xFF,
            (duration) & 0xFF,
            0x55, 0xC4,
            0x00, 0x00 // pre_defined = 0
        ]));
    };
    // Media handler reference box
    MP4.hdlr = function (meta) {
        var data = null;
        if (meta.type === 'audio') {
            data = MP4.constants.HDLR_AUDIO;
        }
        else {
            data = MP4.constants.HDLR_VIDEO;
        }
        return MP4.box(MP4.types.hdlr, data);
    };
    // Media infomation box
    MP4.minf = function (meta) {
        var xmhd = null;
        if (meta.type === 'audio') {
            xmhd = MP4.box(MP4.types.smhd, MP4.constants.SMHD);
        }
        else {
            xmhd = MP4.box(MP4.types.vmhd, MP4.constants.VMHD);
        }
        return MP4.box(MP4.types.minf, xmhd, MP4.dinf(), MP4.stbl(meta));
    };
    // Data infomation box
    MP4.dinf = function () {
        var result = MP4.box(MP4.types.dinf, MP4.box(MP4.types.dref, MP4.constants.DREF));
        return result;
    };
    // Sample table box
    MP4.stbl = function (meta) {
        var result = MP4.box(MP4.types.stbl, // type: stbl
        MP4.stsd(meta), // Sample Description Table
        MP4.box(MP4.types.stts, MP4.constants.STTS), // Time-To-Sample
        MP4.box(MP4.types.stsc, MP4.constants.STSC), // Sample-To-Chunk
        MP4.box(MP4.types.stsz, MP4.constants.STSZ), // Sample size
        MP4.box(MP4.types.stco, MP4.constants.STCO) // Chunk offset
        );
        return result;
    };
    // Sample description box
    MP4.stsd = function (meta) {
        if (meta.type === 'audio') {
            if (meta.codec === 'mp3') {
                return MP4.box(MP4.types.stsd, MP4.constants.STSD_PREFIX, MP4.mp3(meta));
            }
            else if (meta.codec === 'ac-3') {
                return MP4.box(MP4.types.stsd, MP4.constants.STSD_PREFIX, MP4.ac3(meta));
            }
            else if (meta.codec === 'ec-3') {
                return MP4.box(MP4.types.stsd, MP4.constants.STSD_PREFIX, MP4.ec3(meta));
            }
            else if (meta.codec === 'opus') {
                return MP4.box(MP4.types.stsd, MP4.constants.STSD_PREFIX, MP4.Opus(meta));
            }
            else if (meta.codec == 'flac') {
                return MP4.box(MP4.types.stsd, MP4.constants.STSD_PREFIX, MP4.fLaC(meta));
            }
            else if (meta.codec == 'ipcm') {
                return MP4.box(MP4.types.stsd, MP4.constants.STSD_PREFIX, MP4.ipcm(meta));
            }
            // else: aac -> mp4a
            return MP4.box(MP4.types.stsd, MP4.constants.STSD_PREFIX, MP4.mp4a(meta));
        }
        else if (meta.type === 'video' && meta.codec.startsWith('hvc1')) {
            return MP4.box(MP4.types.stsd, MP4.constants.STSD_PREFIX, MP4.hvc1(meta));
        }
        else if (meta.type === 'video' && meta.codec.startsWith('av01')) {
            return MP4.box(MP4.types.stsd, MP4.constants.STSD_PREFIX, MP4.av01(meta));
        }
        else {
            return MP4.box(MP4.types.stsd, MP4.constants.STSD_PREFIX, MP4.avc1(meta));
        }
    };
    MP4.mp3 = function (meta) {
        var channelCount = meta.channelCount;
        var sampleRate = meta.audioSampleRate;
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x01,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, channelCount,
            0x00, 0x10,
            0x00, 0x00, 0x00, 0x00,
            (sampleRate >>> 8) & 0xFF,
            (sampleRate) & 0xFF,
            0x00, 0x00
        ]);
        return MP4.box(MP4.types['.mp3'], data);
    };
    MP4.mp4a = function (meta) {
        var channelCount = meta.channelCount;
        var sampleRate = meta.audioSampleRate;
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x01,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, channelCount,
            0x00, 0x10,
            0x00, 0x00, 0x00, 0x00,
            (sampleRate >>> 8) & 0xFF,
            (sampleRate) & 0xFF,
            0x00, 0x00
        ]);
        return MP4.box(MP4.types.mp4a, data, MP4.esds(meta));
    };
    MP4.ac3 = function (meta) {
        var channelCount = meta.channelCount;
        var sampleRate = meta.audioSampleRate;
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x01,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, channelCount,
            0x00, 0x10,
            0x00, 0x00, 0x00, 0x00,
            (sampleRate >>> 8) & 0xFF,
            (sampleRate) & 0xFF,
            0x00, 0x00
        ]);
        return MP4.box(MP4.types['ac-3'], data, MP4.box(MP4.types.dac3, new Uint8Array(meta.config)));
    };
    MP4.ec3 = function (meta) {
        var channelCount = meta.channelCount;
        var sampleRate = meta.audioSampleRate;
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x01,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, channelCount,
            0x00, 0x10,
            0x00, 0x00, 0x00, 0x00,
            (sampleRate >>> 8) & 0xFF,
            (sampleRate) & 0xFF,
            0x00, 0x00
        ]);
        return MP4.box(MP4.types['ec-3'], data, MP4.box(MP4.types.dec3, new Uint8Array(meta.config)));
    };
    MP4.esds = function (meta) {
        var config = meta.config || [];
        var configSize = config.length;
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x03,
            0x17 + configSize,
            0x00, 0x01,
            0x00,
            0x04,
            0x0F + configSize,
            0x40,
            0x15,
            0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x05 // descriptor_type
        ].concat([
            configSize
        ]).concat(config).concat([
            0x06, 0x01, 0x02 // GASpecificConfig
        ]));
        return MP4.box(MP4.types.esds, data);
    };
    MP4.Opus = function (meta) {
        var channelCount = meta.channelCount;
        var sampleRate = meta.audioSampleRate;
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x01,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, channelCount,
            0x00, 0x10,
            0x00, 0x00, 0x00, 0x00,
            (sampleRate >>> 8) & 0xFF,
            (sampleRate) & 0xFF,
            0x00, 0x00
        ]);
        return MP4.box(MP4.types.Opus, data, MP4.dOps(meta));
    };
    MP4.dOps = function (meta) {
        var channelCount = meta.channelCount;
        var channelConfigCode = meta.channelConfigCode;
        var sampleRate = meta.audioSampleRate;
        if (meta.config) {
            return MP4.box(MP4.types.dOps, meta.config);
        }
        var mapping = [];
        switch (channelConfigCode) {
            case 0x01:
            case 0x02:
                mapping = [0x0];
                break;
            case 0x00: // dualmono
                mapping = [0xFF, 1, 1, 0, 1];
                break;
            case 0x80: // dualmono
                mapping = [0xFF, 2, 0, 0, 1];
                break;
            case 0x03:
                mapping = [0x01, 2, 1, 0, 2, 1];
                break;
            case 0x04:
                mapping = [0x01, 2, 2, 0, 1, 2, 3];
                break;
            case 0x05:
                mapping = [0x01, 3, 2, 0, 4, 1, 2, 3];
                break;
            case 0x06:
                mapping = [0x01, 4, 2, 0, 4, 1, 2, 3, 5];
                break;
            case 0x07:
                mapping = [0x01, 4, 2, 0, 4, 1, 2, 3, 5, 6];
                break;
            case 0x08:
                mapping = [0x01, 5, 3, 0, 6, 1, 2, 3, 4, 5, 7];
                break;
            case 0x82:
                mapping = [0x01, 1, 2, 0, 1];
                break;
            case 0x83:
                mapping = [0x01, 1, 3, 0, 1, 2];
                break;
            case 0x84:
                mapping = [0x01, 1, 4, 0, 1, 2, 3];
                break;
            case 0x85:
                mapping = [0x01, 1, 5, 0, 1, 2, 3, 4];
                break;
            case 0x86:
                mapping = [0x01, 1, 6, 0, 1, 2, 3, 4, 5];
                break;
            case 0x87:
                mapping = [0x01, 1, 7, 0, 1, 2, 3, 4, 5, 6];
                break;
            case 0x88:
                mapping = [0x01, 1, 8, 0, 1, 2, 3, 4, 5, 6, 7];
                break;
        }
        var data = new Uint8Array(__spreadArray([
            0x00,
            channelCount,
            0x00, 0x00,
            (sampleRate >>> 24) & 0xFF,
            (sampleRate >>> 17) & 0xFF,
            (sampleRate >>> 8) & 0xFF,
            (sampleRate >>> 0) & 0xFF,
            0x00, 0x00
        ], mapping, true));
        return MP4.box(MP4.types.dOps, data);
    };
    MP4.fLaC = function (meta) {
        var channelCount = meta.channelCount;
        var sampleRate = Math.min(meta.audioSampleRate, 65535);
        var sampleSize = meta.sampleSize;
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x01,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, channelCount,
            0x00, (sampleSize),
            0x00, 0x00, 0x00, 0x00,
            (sampleRate >>> 8) & 0xFF,
            (sampleRate) & 0xFF,
            0x00, 0x00
        ]);
        return MP4.box(MP4.types.fLaC, data, MP4.dfLa(meta));
    };
    MP4.dfLa = function (meta) {
        var data = new Uint8Array(__spreadArray([
            0x00, 0x00, 0x00, 0x00
        ], meta.config, true));
        return MP4.box(MP4.types.dfLa, data);
    };
    MP4.ipcm = function (meta) {
        var channelCount = meta.channelCount;
        var sampleRate = Math.min(meta.audioSampleRate, 65535);
        var sampleSize = meta.sampleSize;
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x01,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, channelCount,
            0x00, (sampleSize),
            0x00, 0x00, 0x00, 0x00,
            (sampleRate >>> 8) & 0xFF,
            (sampleRate) & 0xFF,
            0x00, 0x00
        ]);
        if (meta.channelCount === 1) {
            return MP4.box(MP4.types.ipcm, data, MP4.pcmC(meta));
        }
        else {
            return MP4.box(MP4.types.ipcm, data, MP4.chnl(meta), MP4.pcmC(meta));
        }
    };
    MP4.chnl = function (meta) {
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x01,
            meta.channelCount,
            0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // omittedChannelsMap
        ]);
        return MP4.box(MP4.types.chnl, data);
    };
    MP4.pcmC = function (meta) {
        var littleEndian = meta.littleEndian ? 0x01 : 0x00;
        var sampleSize = meta.sampleSize;
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            littleEndian, sampleSize
        ]);
        return MP4.box(MP4.types.pcmC, data);
    };
    MP4.avc1 = function (meta) {
        var avcc = meta.avcc;
        var width = meta.codecWidth, height = meta.codecHeight;
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x01,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            (width >>> 8) & 0xFF,
            (width) & 0xFF,
            (height >>> 8) & 0xFF,
            (height) & 0xFF,
            0x00, 0x48, 0x00, 0x00,
            0x00, 0x48, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x01,
            0x0A,
            0x78, 0x71, 0x71, 0x2F,
            0x66, 0x6C, 0x76, 0x2E,
            0x6A, 0x73, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00,
            0x00, 0x18,
            0xFF, 0xFF // pre_defined = -1
        ]);
        return MP4.box(MP4.types.avc1, data, MP4.box(MP4.types.avcC, avcc));
    };
    MP4.hvc1 = function (meta) {
        var hvcc = meta.hvcc;
        var width = meta.codecWidth, height = meta.codecHeight;
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x01,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            (width >>> 8) & 0xFF,
            (width) & 0xFF,
            (height >>> 8) & 0xFF,
            (height) & 0xFF,
            0x00, 0x48, 0x00, 0x00,
            0x00, 0x48, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x01,
            0x0A,
            0x78, 0x71, 0x71, 0x2F,
            0x66, 0x6C, 0x76, 0x2E,
            0x6A, 0x73, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00,
            0x00, 0x18,
            0xFF, 0xFF // pre_defined = -1
        ]);
        return MP4.box(MP4.types.hvc1, data, MP4.box(MP4.types.hvcC, hvcc));
    };
    MP4.av01 = function (meta) {
        var av1c = meta.av1c;
        var width = meta.codecWidth || 192, height = meta.codecHeight || 108;
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x01,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            (width >>> 8) & 0xFF,
            (width) & 0xFF,
            (height >>> 8) & 0xFF,
            (height) & 0xFF,
            0x00, 0x48, 0x00, 0x00,
            0x00, 0x48, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x01,
            0x0A,
            0x78, 0x71, 0x71, 0x2F,
            0x66, 0x6C, 0x76, 0x2E,
            0x6A, 0x73, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00,
            0x00, 0x18,
            0xFF, 0xFF // pre_defined = -1
        ]);
        return MP4.box(MP4.types.av01, data, MP4.box(MP4.types.av1C, av1c));
    };
    // Movie Extends box
    MP4.mvex = function (meta) {
        return MP4.box(MP4.types.mvex, MP4.trex(meta));
    };
    // Track Extends box
    MP4.trex = function (meta) {
        var trackId = meta.id;
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            (trackId >>> 24) & 0xFF,
            (trackId >>> 16) & 0xFF,
            (trackId >>> 8) & 0xFF,
            (trackId) & 0xFF,
            0x00, 0x00, 0x00, 0x01,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x01, 0x00, 0x01 // default_sample_flags
        ]);
        return MP4.box(MP4.types.trex, data);
    };
    // Movie fragment box
    MP4.moof = function (track, baseMediaDecodeTime) {
        return MP4.box(MP4.types.moof, MP4.mfhd(track.sequenceNumber), MP4.traf(track, baseMediaDecodeTime));
    };
    MP4.mfhd = function (sequenceNumber) {
        var data = new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            (sequenceNumber >>> 24) & 0xFF,
            (sequenceNumber >>> 16) & 0xFF,
            (sequenceNumber >>> 8) & 0xFF,
            (sequenceNumber) & 0xFF
        ]);
        return MP4.box(MP4.types.mfhd, data);
    };
    // Track fragment box
    MP4.traf = function (track, baseMediaDecodeTime) {
        var trackId = track.id;
        // Track fragment header box
        var tfhd = MP4.box(MP4.types.tfhd, new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            (trackId >>> 24) & 0xFF,
            (trackId >>> 16) & 0xFF,
            (trackId >>> 8) & 0xFF,
            (trackId) & 0xFF
        ]));
        // Track Fragment Decode Time
        var tfdt = MP4.box(MP4.types.tfdt, new Uint8Array([
            0x00, 0x00, 0x00, 0x00,
            (baseMediaDecodeTime >>> 24) & 0xFF,
            (baseMediaDecodeTime >>> 16) & 0xFF,
            (baseMediaDecodeTime >>> 8) & 0xFF,
            (baseMediaDecodeTime) & 0xFF
        ]));
        var sdtp = MP4.sdtp(track);
        var trun = MP4.trun(track, sdtp.byteLength + 16 + 16 + 8 + 16 + 8 + 8);
        return MP4.box(MP4.types.traf, tfhd, tfdt, trun, sdtp);
    };
    // Sample Dependency Type box
    MP4.sdtp = function (track) {
        var samples = track.samples || [];
        var sampleCount = samples.length;
        var data = new Uint8Array(4 + sampleCount);
        // 0~4 bytes: version(0) & flags
        for (var i = 0; i < sampleCount; i++) {
            var flags = samples[i].flags;
            data[i + 4] = (flags.isLeading << 6) // is_leading: 2 (bit)
                | (flags.dependsOn << 4) // sample_depends_on
                | (flags.isDependedOn << 2) // sample_is_depended_on
                | (flags.hasRedundancy); // sample_has_redundancy
        }
        return MP4.box(MP4.types.sdtp, data);
    };
    // Track fragment run box
    MP4.trun = function (track, offset) {
        var samples = track.samples || [];
        var sampleCount = samples.length;
        var dataSize = 12 + 16 * sampleCount;
        var data = new Uint8Array(dataSize);
        offset += 8 + dataSize;
        data.set([
            0x00, 0x00, 0x0F, 0x01,
            (sampleCount >>> 24) & 0xFF,
            (sampleCount >>> 16) & 0xFF,
            (sampleCount >>> 8) & 0xFF,
            (sampleCount) & 0xFF,
            (offset >>> 24) & 0xFF,
            (offset >>> 16) & 0xFF,
            (offset >>> 8) & 0xFF,
            (offset) & 0xFF
        ], 0);
        for (var i = 0; i < sampleCount; i++) {
            var duration = samples[i].duration;
            var size = samples[i].size;
            var flags = samples[i].flags;
            var cts = samples[i].cts;
            data.set([
                (duration >>> 24) & 0xFF,
                (duration >>> 16) & 0xFF,
                (duration >>> 8) & 0xFF,
                (duration) & 0xFF,
                (size >>> 24) & 0xFF,
                (size >>> 16) & 0xFF,
                (size >>> 8) & 0xFF,
                (size) & 0xFF,
                (flags.isLeading << 2) | flags.dependsOn,
                (flags.isDependedOn << 6) | (flags.hasRedundancy << 4) | flags.isNonSync,
                0x00, 0x00,
                (cts >>> 24) & 0xFF,
                (cts >>> 16) & 0xFF,
                (cts >>> 8) & 0xFF,
                (cts) & 0xFF
            ], 12 + 16 * i);
        }
        return MP4.box(MP4.types.trun, data);
    };
    MP4.mdat = function (data) {
        return MP4.box(MP4.types.mdat, data);
    };
    return MP4;
}());
MP4.init();
/* harmony default export */ __webpack_exports__["default"] = (MP4);


/***/ }),

/***/ "./src/remux/mp4-remuxer.js":
/*!**********************************!*\
  !*** ./src/remux/mp4-remuxer.js ***!
  \**********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! ../utils/logger.js */ "./src/utils/logger.js");
/* harmony import */ var _mp4_generator_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ./mp4-generator.js */ "./src/remux/mp4-generator.js");
/* harmony import */ var _aac_silent_js__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__(/*! ./aac-silent.js */ "./src/remux/aac-silent.js");
/* harmony import */ var _utils_browser_js__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__(/*! ../utils/browser.js */ "./src/utils/browser.js");
/* harmony import */ var _core_media_segment_info_js__WEBPACK_IMPORTED_MODULE_4__ = __webpack_require__(/*! ../core/media-segment-info.js */ "./src/core/media-segment-info.js");
/* harmony import */ var _utils_exception_js__WEBPACK_IMPORTED_MODULE_5__ = __webpack_require__(/*! ../utils/exception.js */ "./src/utils/exception.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */






// Fragmented mp4 remuxer
var MP4Remuxer = /** @class */ (function () {
    function MP4Remuxer(config) {
        this.TAG = 'MP4Remuxer';
        this._config = config;
        this._isLive = (config.isLive === true) ? true : false;
        this._dtsBase = -1;
        this._dtsBaseInited = false;
        this._audioDtsBase = Infinity;
        this._videoDtsBase = Infinity;
        this._audioNextDts = undefined;
        this._videoNextDts = undefined;
        this._audioStashedLastSample = null;
        this._videoStashedLastSample = null;
        this._audioMeta = null;
        this._videoMeta = null;
        this._audioSegmentInfoList = new _core_media_segment_info_js__WEBPACK_IMPORTED_MODULE_4__["MediaSegmentInfoList"]('audio');
        this._videoSegmentInfoList = new _core_media_segment_info_js__WEBPACK_IMPORTED_MODULE_4__["MediaSegmentInfoList"]('video');
        this._onInitSegment = null;
        this._onMediaSegment = null;
        // Workaround for chrome < 50: Always force first sample as a Random Access Point in media segment
        // see https://bugs.chromium.org/p/chromium/issues/detail?id=229412
        this._forceFirstIDR = (_utils_browser_js__WEBPACK_IMPORTED_MODULE_3__["default"].chrome &&
            (_utils_browser_js__WEBPACK_IMPORTED_MODULE_3__["default"].version.major < 50 ||
                (_utils_browser_js__WEBPACK_IMPORTED_MODULE_3__["default"].version.major === 50 && _utils_browser_js__WEBPACK_IMPORTED_MODULE_3__["default"].version.build < 2661))) ? true : false;
        // Workaround for IE11/Edge: Fill silent aac frame after keyframe-seeking
        // Make audio beginDts equals with video beginDts, in order to fix seek freeze
        this._fillSilentAfterSeek = (_utils_browser_js__WEBPACK_IMPORTED_MODULE_3__["default"].msedge || _utils_browser_js__WEBPACK_IMPORTED_MODULE_3__["default"].msie);
        // While only FireFox supports 'audio/mp4, codecs="mp3"', use 'audio/mpeg' for chrome, safari, ...
        this._mp3UseMpegAudio = !_utils_browser_js__WEBPACK_IMPORTED_MODULE_3__["default"].firefox;
        this._fillAudioTimestampGap = this._config.fixAudioTimestampGap;
    }
    MP4Remuxer.prototype.destroy = function () {
        this._dtsBase = -1;
        this._dtsBaseInited = false;
        this._audioMeta = null;
        this._videoMeta = null;
        this._audioSegmentInfoList.clear();
        this._audioSegmentInfoList = null;
        this._videoSegmentInfoList.clear();
        this._videoSegmentInfoList = null;
        this._onInitSegment = null;
        this._onMediaSegment = null;
    };
    MP4Remuxer.prototype.bindDataSource = function (producer) {
        producer.onDataAvailable = this.remux.bind(this);
        producer.onTrackMetadata = this._onTrackMetadataReceived.bind(this);
        return this;
    };
    Object.defineProperty(MP4Remuxer.prototype, "onInitSegment", {
        /* prototype: function onInitSegment(type: string, initSegment: ArrayBuffer): void
           InitSegment: {
               type: string,
               data: ArrayBuffer,
               codec: string,
               container: string
           }
        */
        get: function () {
            return this._onInitSegment;
        },
        set: function (callback) {
            this._onInitSegment = callback;
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(MP4Remuxer.prototype, "onMediaSegment", {
        /* prototype: function onMediaSegment(type: string, mediaSegment: MediaSegment): void
           MediaSegment: {
               type: string,
               data: ArrayBuffer,
               sampleCount: int32
               info: MediaSegmentInfo
           }
        */
        get: function () {
            return this._onMediaSegment;
        },
        set: function (callback) {
            this._onMediaSegment = callback;
        },
        enumerable: false,
        configurable: true
    });
    MP4Remuxer.prototype.insertDiscontinuity = function () {
        this._audioNextDts = this._videoNextDts = undefined;
    };
    MP4Remuxer.prototype.seek = function (originalDts) {
        this._audioStashedLastSample = null;
        this._videoStashedLastSample = null;
        this._videoSegmentInfoList.clear();
        this._audioSegmentInfoList.clear();
    };
    MP4Remuxer.prototype.remux = function (audioTrack, videoTrack) {
        if (!this._onMediaSegment) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_5__["IllegalStateException"]('MP4Remuxer: onMediaSegment callback must be specificed!');
        }
        if (!this._dtsBaseInited) {
            this._calculateDtsBase(audioTrack, videoTrack);
        }
        if (videoTrack) {
            this._remuxVideo(videoTrack);
        }
        if (audioTrack) {
            this._remuxAudio(audioTrack);
        }
    };
    MP4Remuxer.prototype._onTrackMetadataReceived = function (type, metadata) {
        var metabox = null;
        var container = 'mp4';
        var codec = metadata.codec;
        if (type === 'audio') {
            this._audioMeta = metadata;
            if (metadata.codec === 'mp3' && this._mp3UseMpegAudio) {
                // 'audio/mpeg' for MP3 audio track
                container = 'mpeg';
                codec = '';
                metabox = new Uint8Array();
            }
            else {
                // 'audio/mp4, codecs="codec"'
                metabox = _mp4_generator_js__WEBPACK_IMPORTED_MODULE_1__["default"].generateInitSegment(metadata);
            }
        }
        else if (type === 'video') {
            this._videoMeta = metadata;
            metabox = _mp4_generator_js__WEBPACK_IMPORTED_MODULE_1__["default"].generateInitSegment(metadata);
        }
        else {
            return;
        }
        // dispatch metabox (Initialization Segment)
        if (!this._onInitSegment) {
            throw new _utils_exception_js__WEBPACK_IMPORTED_MODULE_5__["IllegalStateException"]('MP4Remuxer: onInitSegment callback must be specified!');
        }
        this._onInitSegment(type, {
            type: type,
            data: metabox.buffer,
            codec: codec,
            container: "".concat(type, "/").concat(container),
            mediaDuration: metadata.duration // in timescale 1000 (milliseconds)
        });
    };
    MP4Remuxer.prototype._calculateDtsBase = function (audioTrack, videoTrack) {
        if (this._dtsBaseInited) {
            return;
        }
        if (audioTrack && audioTrack.samples && audioTrack.samples.length) {
            this._audioDtsBase = audioTrack.samples[0].dts;
        }
        if (videoTrack && videoTrack.samples && videoTrack.samples.length) {
            this._videoDtsBase = videoTrack.samples[0].dts;
        }
        this._dtsBase = Math.min(this._audioDtsBase, this._videoDtsBase);
        this._dtsBaseInited = true;
    };
    MP4Remuxer.prototype.getTimestampBase = function () {
        if (!this._dtsBaseInited) {
            return undefined;
        }
        return this._dtsBase;
    };
    MP4Remuxer.prototype.flushStashedSamples = function () {
        var videoSample = this._videoStashedLastSample;
        var audioSample = this._audioStashedLastSample;
        var videoTrack = {
            type: 'video',
            id: 1,
            sequenceNumber: 0,
            samples: [],
            length: 0
        };
        if (videoSample != null) {
            videoTrack.samples.push(videoSample);
            videoTrack.length = videoSample.length;
        }
        var audioTrack = {
            type: 'audio',
            id: 2,
            sequenceNumber: 0,
            samples: [],
            length: 0
        };
        if (audioSample != null) {
            audioTrack.samples.push(audioSample);
            audioTrack.length = audioSample.length;
        }
        this._videoStashedLastSample = null;
        this._audioStashedLastSample = null;
        this._remuxVideo(videoTrack, true);
        this._remuxAudio(audioTrack, true);
    };
    MP4Remuxer.prototype._remuxAudio = function (audioTrack, force) {
        if (this._audioMeta == null) {
            return;
        }
        var track = audioTrack;
        var samples = track.samples;
        var dtsCorrection = undefined;
        var firstDts = -1, lastDts = -1, lastPts = -1;
        var refSampleDuration = this._audioMeta.refSampleDuration;
        var mpegRawTrack = this._audioMeta.codec === 'mp3' && this._mp3UseMpegAudio;
        var firstSegmentAfterSeek = this._dtsBaseInited && this._audioNextDts === undefined;
        var insertPrefixSilentFrame = false;
        if (!samples || samples.length === 0) {
            return;
        }
        if (samples.length === 1 && !force) {
            // If [sample count in current batch] === 1 && (force != true)
            // Ignore and keep in demuxer's queue
            return;
        } // else if (force === true) do remux
        var offset = 0;
        var mdatbox = null;
        var mdatBytes = 0;
        // calculate initial mdat size
        if (mpegRawTrack) {
            // for raw mpeg buffer
            offset = 0;
            mdatBytes = track.length;
        }
        else {
            // for fmp4 mdat box
            offset = 8; // size + type
            mdatBytes = 8 + track.length;
        }
        var lastSample = null;
        // Pop the lastSample and waiting for stash
        if (samples.length > 1) {
            lastSample = samples.pop();
            mdatBytes -= lastSample.length;
        }
        // Insert [stashed lastSample in the previous batch] to the front
        if (this._audioStashedLastSample != null) {
            var sample = this._audioStashedLastSample;
            this._audioStashedLastSample = null;
            samples.unshift(sample);
            mdatBytes += sample.length;
        }
        // Stash the lastSample of current batch, waiting for next batch
        if (lastSample != null) {
            this._audioStashedLastSample = lastSample;
        }
        var firstSampleOriginalDts = samples[0].dts - this._dtsBase;
        // calculate dtsCorrection
        if (this._audioNextDts) {
            dtsCorrection = firstSampleOriginalDts - this._audioNextDts;
        }
        else { // this._audioNextDts == undefined
            if (this._audioSegmentInfoList.isEmpty()) {
                dtsCorrection = 0;
                if (this._fillSilentAfterSeek && !this._videoSegmentInfoList.isEmpty()) {
                    if (this._audioMeta.originalCodec !== 'mp3') {
                        insertPrefixSilentFrame = true;
                    }
                }
            }
            else {
                var lastSample_1 = this._audioSegmentInfoList.getLastSampleBefore(firstSampleOriginalDts);
                if (lastSample_1 != null) {
                    var distance = (firstSampleOriginalDts - (lastSample_1.originalDts + lastSample_1.duration));
                    if (distance <= 3) {
                        distance = 0;
                    }
                    var expectedDts = lastSample_1.dts + lastSample_1.duration + distance;
                    dtsCorrection = firstSampleOriginalDts - expectedDts;
                }
                else { // lastSample == null, cannot found
                    dtsCorrection = 0;
                }
            }
        }
        if (insertPrefixSilentFrame) {
            // align audio segment beginDts to match with current video segment's beginDts
            var firstSampleDts = firstSampleOriginalDts - dtsCorrection;
            var videoSegment = this._videoSegmentInfoList.getLastSegmentBefore(firstSampleOriginalDts);
            if (videoSegment != null && videoSegment.beginDts < firstSampleDts) {
                var silentUnit = _aac_silent_js__WEBPACK_IMPORTED_MODULE_2__["default"].getSilentFrame(this._audioMeta.originalCodec, this._audioMeta.channelCount);
                if (silentUnit) {
                    var dts = videoSegment.beginDts;
                    var silentFrameDuration = firstSampleDts - videoSegment.beginDts;
                    _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].v(this.TAG, "InsertPrefixSilentAudio: dts: ".concat(dts, ", duration: ").concat(silentFrameDuration));
                    samples.unshift({ unit: silentUnit, dts: dts, pts: dts });
                    mdatBytes += silentUnit.byteLength;
                } // silentUnit == null: Cannot generate, skip
            }
            else {
                insertPrefixSilentFrame = false;
            }
        }
        var mp4Samples = [];
        // Correct dts for each sample, and calculate sample duration. Then output to mp4Samples
        for (var i = 0; i < samples.length; i++) {
            var sample = samples[i];
            var unit = sample.unit;
            var originalDts = sample.dts - this._dtsBase;
            var dts = originalDts;
            var needFillSilentFrames = false;
            var silentFrames = null;
            var sampleDuration = 0;
            if (originalDts < -0.001) {
                continue; //pass the first sample with the invalid dts
            }
            if (this._audioMeta.codec !== 'mp3' && refSampleDuration != null) {
                // for AAC codec, we need to keep dts increase based on refSampleDuration
                var curRefDts = originalDts;
                var maxAudioFramesDrift = 3;
                if (this._audioNextDts) {
                    curRefDts = this._audioNextDts;
                }
                dtsCorrection = originalDts - curRefDts;
                if (dtsCorrection <= -maxAudioFramesDrift * refSampleDuration) {
                    // If we're overlapping by more than maxAudioFramesDrift number of frame, drop this sample
                    _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, "Dropping 1 audio frame (originalDts: ".concat(originalDts, " ms ,curRefDts: ").concat(curRefDts, " ms)  due to dtsCorrection: ").concat(dtsCorrection, " ms overlap."));
                    continue;
                }
                else if (dtsCorrection >= maxAudioFramesDrift * refSampleDuration && this._fillAudioTimestampGap && !_utils_browser_js__WEBPACK_IMPORTED_MODULE_3__["default"].safari) {
                    // Silent frame generation, if large timestamp gap detected && config.fixAudioTimestampGap
                    needFillSilentFrames = true;
                    // We need to insert silent frames to fill timestamp gap
                    var frameCount = Math.floor(dtsCorrection / refSampleDuration);
                    _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Large audio timestamp gap detected, may cause AV sync to drift. ' +
                        'Silent frames will be generated to avoid unsync.\n' +
                        "originalDts: ".concat(originalDts, " ms, curRefDts: ").concat(curRefDts, " ms, ") +
                        "dtsCorrection: ".concat(Math.round(dtsCorrection), " ms, generate: ").concat(frameCount, " frames"));
                    dts = Math.floor(curRefDts);
                    sampleDuration = Math.floor(curRefDts + refSampleDuration) - dts;
                    var silentUnit = _aac_silent_js__WEBPACK_IMPORTED_MODULE_2__["default"].getSilentFrame(this._audioMeta.originalCodec, this._audioMeta.channelCount);
                    if (silentUnit == null) {
                        _utils_logger_js__WEBPACK_IMPORTED_MODULE_0__["default"].w(this.TAG, 'Unable to generate silent frame for ' +
                            "".concat(this._audioMeta.originalCodec, " with ").concat(this._audioMeta.channelCount, " channels, repeat last frame"));
                        // Repeat last frame
                        silentUnit = unit;
                    }
                    silentFrames = [];
                    for (var j = 0; j < frameCount; j++) {
                        curRefDts = curRefDts + refSampleDuration;
                        var intDts = Math.floor(curRefDts); // change to integer
                        var intDuration = Math.floor(curRefDts + refSampleDuration) - intDts;
                        var frame = {
                            dts: intDts,
                            pts: intDts,
                            cts: 0,
                            unit: silentUnit,
                            size: silentUnit.byteLength,
                            duration: intDuration,
                            originalDts: originalDts,
                            flags: {
                                isLeading: 0,
                                dependsOn: 1,
                                isDependedOn: 0,
                                hasRedundancy: 0
                            }
                        };
                        silentFrames.push(frame);
                        mdatBytes += frame.size;
                    }
                    this._audioNextDts = curRefDts + refSampleDuration;
                }
                else {
                    dts = Math.floor(curRefDts);
                    sampleDuration = Math.floor(curRefDts + refSampleDuration) - dts;
                    this._audioNextDts = curRefDts + refSampleDuration;
                }
            }
            else {
                // keep the original dts calculate algorithm for mp3
                dts = originalDts - dtsCorrection;
                if (i !== samples.length - 1) {
                    var nextDts = samples[i + 1].dts - this._dtsBase - dtsCorrection;
                    sampleDuration = nextDts - dts;
                }
                else { // the last sample
                    if (lastSample != null) { // use stashed sample's dts to calculate sample duration
                        var nextDts = lastSample.dts - this._dtsBase - dtsCorrection;
                        sampleDuration = nextDts - dts;
                    }
                    else if (mp4Samples.length >= 1) { // use second last sample duration
                        sampleDuration = mp4Samples[mp4Samples.length - 1].duration;
                    }
                    else { // the only one sample, use reference sample duration
                        sampleDuration = Math.floor(refSampleDuration);
                    }
                }
                this._audioNextDts = dts + sampleDuration;
            }
            if (firstDts === -1) {
                firstDts = dts;
            }
            mp4Samples.push({
                dts: dts,
                pts: dts,
                cts: 0,
                unit: sample.unit,
                size: sample.unit.byteLength,
                duration: sampleDuration,
                originalDts: originalDts,
                flags: {
                    isLeading: 0,
                    dependsOn: 1,
                    isDependedOn: 0,
                    hasRedundancy: 0
                }
            });
            if (needFillSilentFrames) {
                // Silent frames should be inserted after wrong-duration frame
                mp4Samples.push.apply(mp4Samples, silentFrames);
            }
        }
        if (mp4Samples.length === 0) {
            //no samples need to remux
            track.samples = [];
            track.length = 0;
            return;
        }
        // allocate mdatbox
        if (mpegRawTrack) {
            // allocate for raw mpeg buffer
            mdatbox = new Uint8Array(mdatBytes);
        }
        else {
            // allocate for fmp4 mdat box
            mdatbox = new Uint8Array(mdatBytes);
            // size field
            mdatbox[0] = (mdatBytes >>> 24) & 0xFF;
            mdatbox[1] = (mdatBytes >>> 16) & 0xFF;
            mdatbox[2] = (mdatBytes >>> 8) & 0xFF;
            mdatbox[3] = (mdatBytes) & 0xFF;
            // type field (fourCC)
            mdatbox.set(_mp4_generator_js__WEBPACK_IMPORTED_MODULE_1__["default"].types.mdat, 4);
        }
        // Write samples into mdatbox
        for (var i = 0; i < mp4Samples.length; i++) {
            var unit = mp4Samples[i].unit;
            mdatbox.set(unit, offset);
            offset += unit.byteLength;
        }
        var latest = mp4Samples[mp4Samples.length - 1];
        lastDts = latest.dts + latest.duration;
        //this._audioNextDts = lastDts;
        // fill media segment info & add to info list
        var info = new _core_media_segment_info_js__WEBPACK_IMPORTED_MODULE_4__["MediaSegmentInfo"]();
        info.beginDts = firstDts;
        info.endDts = lastDts;
        info.beginPts = firstDts;
        info.endPts = lastDts;
        info.originalBeginDts = mp4Samples[0].originalDts;
        info.originalEndDts = latest.originalDts + latest.duration;
        info.firstSample = new _core_media_segment_info_js__WEBPACK_IMPORTED_MODULE_4__["SampleInfo"](mp4Samples[0].dts, mp4Samples[0].pts, mp4Samples[0].duration, mp4Samples[0].originalDts, false);
        info.lastSample = new _core_media_segment_info_js__WEBPACK_IMPORTED_MODULE_4__["SampleInfo"](latest.dts, latest.pts, latest.duration, latest.originalDts, false);
        if (!this._isLive) {
            this._audioSegmentInfoList.append(info);
        }
        track.samples = mp4Samples;
        track.sequenceNumber++;
        var moofbox = null;
        if (mpegRawTrack) {
            // Generate empty buffer, because useless for raw mpeg
            moofbox = new Uint8Array();
        }
        else {
            // Generate moof for fmp4 segment
            moofbox = _mp4_generator_js__WEBPACK_IMPORTED_MODULE_1__["default"].moof(track, firstDts);
        }
        track.samples = [];
        track.length = 0;
        var segment = {
            type: 'audio',
            data: this._mergeBoxes(moofbox, mdatbox).buffer,
            sampleCount: mp4Samples.length,
            info: info
        };
        if (mpegRawTrack && firstSegmentAfterSeek) {
            // For MPEG audio stream in MSE, if seeking occurred, before appending new buffer
            // We need explicitly set timestampOffset to the desired point in timeline for mpeg SourceBuffer.
            segment.timestampOffset = firstDts;
        }
        this._onMediaSegment('audio', segment);
    };
    MP4Remuxer.prototype._remuxVideo = function (videoTrack, force) {
        if (this._videoMeta == null) {
            return;
        }
        var track = videoTrack;
        var samples = track.samples;
        var dtsCorrection = undefined;
        var firstDts = -1, lastDts = -1;
        var firstPts = -1, lastPts = -1;
        if (!samples || samples.length === 0) {
            return;
        }
        if (samples.length === 1 && !force) {
            // If [sample count in current batch] === 1 && (force != true)
            // Ignore and keep in demuxer's queue
            return;
        } // else if (force === true) do remux
        var offset = 8;
        var mdatbox = null;
        var mdatBytes = 8 + videoTrack.length;
        var lastSample = null;
        // Pop the lastSample and waiting for stash
        if (samples.length > 1) {
            lastSample = samples.pop();
            mdatBytes -= lastSample.length;
        }
        // Insert [stashed lastSample in the previous batch] to the front
        if (this._videoStashedLastSample != null) {
            var sample = this._videoStashedLastSample;
            this._videoStashedLastSample = null;
            samples.unshift(sample);
            mdatBytes += sample.length;
        }
        // Stash the lastSample of current batch, waiting for next batch
        if (lastSample != null) {
            this._videoStashedLastSample = lastSample;
        }
        var firstSampleOriginalDts = samples[0].dts - this._dtsBase;
        // calculate dtsCorrection
        if (this._videoNextDts) {
            dtsCorrection = firstSampleOriginalDts - this._videoNextDts;
        }
        else { // this._videoNextDts == undefined
            if (this._videoSegmentInfoList.isEmpty()) {
                dtsCorrection = 0;
            }
            else {
                var lastSample_2 = this._videoSegmentInfoList.getLastSampleBefore(firstSampleOriginalDts);
                if (lastSample_2 != null) {
                    var distance = (firstSampleOriginalDts - (lastSample_2.originalDts + lastSample_2.duration));
                    if (distance <= 3) {
                        distance = 0;
                    }
                    var expectedDts = lastSample_2.dts + lastSample_2.duration + distance;
                    dtsCorrection = firstSampleOriginalDts - expectedDts;
                }
                else { // lastSample == null, cannot found
                    dtsCorrection = 0;
                }
            }
        }
        var info = new _core_media_segment_info_js__WEBPACK_IMPORTED_MODULE_4__["MediaSegmentInfo"]();
        var mp4Samples = [];
        // Correct dts for each sample, and calculate sample duration. Then output to mp4Samples
        for (var i = 0; i < samples.length; i++) {
            var sample = samples[i];
            var originalDts = sample.dts - this._dtsBase;
            var isKeyframe = sample.isKeyframe;
            var dts = originalDts - dtsCorrection;
            var cts = sample.cts;
            var pts = dts + cts;
            if (firstDts === -1) {
                firstDts = dts;
                firstPts = pts;
            }
            var sampleDuration = 0;
            if (i !== samples.length - 1) {
                var nextDts = samples[i + 1].dts - this._dtsBase - dtsCorrection;
                sampleDuration = nextDts - dts;
            }
            else { // the last sample
                if (lastSample != null) { // use stashed sample's dts to calculate sample duration
                    var nextDts = lastSample.dts - this._dtsBase - dtsCorrection;
                    sampleDuration = nextDts - dts;
                }
                else if (mp4Samples.length >= 1) { // use second last sample duration
                    sampleDuration = mp4Samples[mp4Samples.length - 1].duration;
                }
                else { // the only one sample, use reference sample duration
                    sampleDuration = Math.floor(this._videoMeta.refSampleDuration);
                }
            }
            if (isKeyframe) {
                var syncPoint = new _core_media_segment_info_js__WEBPACK_IMPORTED_MODULE_4__["SampleInfo"](dts, pts, sampleDuration, sample.dts, true);
                syncPoint.fileposition = sample.fileposition;
                info.appendSyncPoint(syncPoint);
            }
            mp4Samples.push({
                dts: dts,
                pts: pts,
                cts: cts,
                units: sample.units,
                size: sample.length,
                isKeyframe: isKeyframe,
                duration: sampleDuration,
                originalDts: originalDts,
                flags: {
                    isLeading: 0,
                    dependsOn: isKeyframe ? 2 : 1,
                    isDependedOn: isKeyframe ? 1 : 0,
                    hasRedundancy: 0,
                    isNonSync: isKeyframe ? 0 : 1
                }
            });
        }
        // allocate mdatbox
        mdatbox = new Uint8Array(mdatBytes);
        mdatbox[0] = (mdatBytes >>> 24) & 0xFF;
        mdatbox[1] = (mdatBytes >>> 16) & 0xFF;
        mdatbox[2] = (mdatBytes >>> 8) & 0xFF;
        mdatbox[3] = (mdatBytes) & 0xFF;
        mdatbox.set(_mp4_generator_js__WEBPACK_IMPORTED_MODULE_1__["default"].types.mdat, 4);
        // Write samples into mdatbox
        for (var i = 0; i < mp4Samples.length; i++) {
            var units = mp4Samples[i].units;
            while (units.length) {
                var unit = units.shift();
                var data = unit.data;
                mdatbox.set(data, offset);
                offset += data.byteLength;
            }
        }
        var latest = mp4Samples[mp4Samples.length - 1];
        lastDts = latest.dts + latest.duration;
        lastPts = latest.pts + latest.duration;
        this._videoNextDts = lastDts;
        // fill media segment info & add to info list
        info.beginDts = firstDts;
        info.endDts = lastDts;
        info.beginPts = firstPts;
        info.endPts = lastPts;
        info.originalBeginDts = mp4Samples[0].originalDts;
        info.originalEndDts = latest.originalDts + latest.duration;
        info.firstSample = new _core_media_segment_info_js__WEBPACK_IMPORTED_MODULE_4__["SampleInfo"](mp4Samples[0].dts, mp4Samples[0].pts, mp4Samples[0].duration, mp4Samples[0].originalDts, mp4Samples[0].isKeyframe);
        info.lastSample = new _core_media_segment_info_js__WEBPACK_IMPORTED_MODULE_4__["SampleInfo"](latest.dts, latest.pts, latest.duration, latest.originalDts, latest.isKeyframe);
        if (!this._isLive) {
            this._videoSegmentInfoList.append(info);
        }
        track.samples = mp4Samples;
        track.sequenceNumber++;
        // workaround for chrome < 50: force first sample as a random access point
        // see https://bugs.chromium.org/p/chromium/issues/detail?id=229412
        if (this._forceFirstIDR) {
            var flags = mp4Samples[0].flags;
            flags.dependsOn = 2;
            flags.isNonSync = 0;
        }
        var moofbox = _mp4_generator_js__WEBPACK_IMPORTED_MODULE_1__["default"].moof(track, firstDts);
        track.samples = [];
        track.length = 0;
        this._onMediaSegment('video', {
            type: 'video',
            data: this._mergeBoxes(moofbox, mdatbox).buffer,
            sampleCount: mp4Samples.length,
            info: info
        });
    };
    MP4Remuxer.prototype._mergeBoxes = function (moof, mdat) {
        var result = new Uint8Array(moof.byteLength + mdat.byteLength);
        result.set(moof, 0);
        result.set(mdat, moof.byteLength);
        return result;
    };
    return MP4Remuxer;
}());
/* harmony default export */ __webpack_exports__["default"] = (MP4Remuxer);


/***/ }),

/***/ "./src/utils/browser.js":
/*!******************************!*\
  !*** ./src/utils/browser.js ***!
  \******************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var Browser = {};
function detect() {
    // modified from jquery-browser-plugin
    var ua = self.navigator.userAgent.toLowerCase();
    var match = /(edge)\/([\w.]+)/.exec(ua) ||
        /(opr)[\/]([\w.]+)/.exec(ua) ||
        /(chrome)[ \/]([\w.]+)/.exec(ua) ||
        /(iemobile)[\/]([\w.]+)/.exec(ua) ||
        /(version)(applewebkit)[ \/]([\w.]+).*(safari)[ \/]([\w.]+)/.exec(ua) ||
        /(webkit)[ \/]([\w.]+).*(version)[ \/]([\w.]+).*(safari)[ \/]([\w.]+)/.exec(ua) ||
        /(webkit)[ \/]([\w.]+)/.exec(ua) ||
        /(opera)(?:.*version|)[ \/]([\w.]+)/.exec(ua) ||
        /(msie) ([\w.]+)/.exec(ua) ||
        ua.indexOf('trident') >= 0 && /(rv)(?::| )([\w.]+)/.exec(ua) ||
        ua.indexOf('compatible') < 0 && /(firefox)[ \/]([\w.]+)/.exec(ua) ||
        [];
    var platform_match = /(ipad)/.exec(ua) ||
        /(ipod)/.exec(ua) ||
        /(windows phone)/.exec(ua) ||
        /(iphone)/.exec(ua) ||
        /(kindle)/.exec(ua) ||
        /(android)/.exec(ua) ||
        /(windows)/.exec(ua) ||
        /(mac)/.exec(ua) ||
        /(linux)/.exec(ua) ||
        /(cros)/.exec(ua) ||
        [];
    var matched = {
        browser: match[5] || match[3] || match[1] || '',
        version: match[2] || match[4] || '0',
        majorVersion: match[4] || match[2] || '0',
        platform: platform_match[0] || ''
    };
    var browser = {};
    if (matched.browser) {
        browser[matched.browser] = true;
        var versionArray = matched.majorVersion.split('.');
        browser.version = {
            major: parseInt(matched.majorVersion, 10),
            string: matched.version
        };
        if (versionArray.length > 1) {
            browser.version.minor = parseInt(versionArray[1], 10);
        }
        if (versionArray.length > 2) {
            browser.version.build = parseInt(versionArray[2], 10);
        }
    }
    if (matched.platform) {
        browser[matched.platform] = true;
    }
    if (browser.chrome || browser.opr || browser.safari) {
        browser.webkit = true;
    }
    // MSIE. IE11 has 'rv' identifer
    if (browser.rv || browser.iemobile) {
        if (browser.rv) {
            delete browser.rv;
        }
        var msie = 'msie';
        matched.browser = msie;
        browser[msie] = true;
    }
    // Microsoft Edge
    if (browser.edge) {
        delete browser.edge;
        var msedge = 'msedge';
        matched.browser = msedge;
        browser[msedge] = true;
    }
    // Opera 15+
    if (browser.opr) {
        var opera = 'opera';
        matched.browser = opera;
        browser[opera] = true;
    }
    // Stock android browsers are marked as Safari
    if (browser.safari && browser.android) {
        var android = 'android';
        matched.browser = android;
        browser[android] = true;
    }
    browser.name = matched.browser;
    browser.platform = matched.platform;
    for (var key in Browser) {
        if (Browser.hasOwnProperty(key)) {
            delete Browser[key];
        }
    }
    Object.assign(Browser, browser);
}
detect();
/* harmony default export */ __webpack_exports__["default"] = (Browser);


/***/ }),

/***/ "./src/utils/exception.js":
/*!********************************!*\
  !*** ./src/utils/exception.js ***!
  \********************************/
/*! exports provided: RuntimeException, IllegalStateException, InvalidArgumentException, NotImplementedException */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "RuntimeException", function() { return RuntimeException; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "IllegalStateException", function() { return IllegalStateException; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "InvalidArgumentException", function() { return InvalidArgumentException; });
/* harmony export (binding) */ __webpack_require__.d(__webpack_exports__, "NotImplementedException", function() { return NotImplementedException; });
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var __extends = (undefined && undefined.__extends) || (function () {
    var extendStatics = function (d, b) {
        extendStatics = Object.setPrototypeOf ||
            ({ __proto__: [] } instanceof Array && function (d, b) { d.__proto__ = b; }) ||
            function (d, b) { for (var p in b) if (Object.prototype.hasOwnProperty.call(b, p)) d[p] = b[p]; };
        return extendStatics(d, b);
    };
    return function (d, b) {
        if (typeof b !== "function" && b !== null)
            throw new TypeError("Class extends value " + String(b) + " is not a constructor or null");
        extendStatics(d, b);
        function __() { this.constructor = d; }
        d.prototype = b === null ? Object.create(b) : (__.prototype = b.prototype, new __());
    };
})();
var RuntimeException = /** @class */ (function () {
    function RuntimeException(message) {
        this._message = message;
    }
    Object.defineProperty(RuntimeException.prototype, "name", {
        get: function () {
            return 'RuntimeException';
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(RuntimeException.prototype, "message", {
        get: function () {
            return this._message;
        },
        enumerable: false,
        configurable: true
    });
    RuntimeException.prototype.toString = function () {
        return this.name + ': ' + this.message;
    };
    return RuntimeException;
}());

var IllegalStateException = /** @class */ (function (_super) {
    __extends(IllegalStateException, _super);
    function IllegalStateException(message) {
        return _super.call(this, message) || this;
    }
    Object.defineProperty(IllegalStateException.prototype, "name", {
        get: function () {
            return 'IllegalStateException';
        },
        enumerable: false,
        configurable: true
    });
    return IllegalStateException;
}(RuntimeException));

var InvalidArgumentException = /** @class */ (function (_super) {
    __extends(InvalidArgumentException, _super);
    function InvalidArgumentException(message) {
        return _super.call(this, message) || this;
    }
    Object.defineProperty(InvalidArgumentException.prototype, "name", {
        get: function () {
            return 'InvalidArgumentException';
        },
        enumerable: false,
        configurable: true
    });
    return InvalidArgumentException;
}(RuntimeException));

var NotImplementedException = /** @class */ (function (_super) {
    __extends(NotImplementedException, _super);
    function NotImplementedException(message) {
        return _super.call(this, message) || this;
    }
    Object.defineProperty(NotImplementedException.prototype, "name", {
        get: function () {
            return 'NotImplementedException';
        },
        enumerable: false,
        configurable: true
    });
    return NotImplementedException;
}(RuntimeException));



/***/ }),

/***/ "./src/utils/logger.js":
/*!*****************************!*\
  !*** ./src/utils/logger.js ***!
  \*****************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! events */ "./node_modules/events/events.js");
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0___default = /*#__PURE__*/__webpack_require__.n(events__WEBPACK_IMPORTED_MODULE_0__);
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

var Log = /** @class */ (function () {
    function Log() {
    }
    Log.e = function (tag, msg) {
        if (!tag || Log.FORCE_GLOBAL_TAG)
            tag = Log.GLOBAL_TAG;
        var str = "[".concat(tag, "] > ").concat(msg);
        if (Log.ENABLE_CALLBACK) {
            Log.emitter.emit('log', 'error', str);
        }
        if (!Log.ENABLE_ERROR) {
            return;
        }
        if (console.error) {
            console.error(str);
        }
        else if (console.warn) {
            console.warn(str);
        }
        else {
            console.log(str);
        }
    };
    Log.i = function (tag, msg) {
        if (!tag || Log.FORCE_GLOBAL_TAG)
            tag = Log.GLOBAL_TAG;
        var str = "[".concat(tag, "] > ").concat(msg);
        if (Log.ENABLE_CALLBACK) {
            Log.emitter.emit('log', 'info', str);
        }
        if (!Log.ENABLE_INFO) {
            return;
        }
        if (console.info) {
            console.info(str);
        }
        else {
            console.log(str);
        }
    };
    Log.w = function (tag, msg) {
        if (!tag || Log.FORCE_GLOBAL_TAG)
            tag = Log.GLOBAL_TAG;
        var str = "[".concat(tag, "] > ").concat(msg);
        if (Log.ENABLE_CALLBACK) {
            Log.emitter.emit('log', 'warn', str);
        }
        if (!Log.ENABLE_WARN) {
            return;
        }
        if (console.warn) {
            console.warn(str);
        }
        else {
            console.log(str);
        }
    };
    Log.d = function (tag, msg) {
        if (!tag || Log.FORCE_GLOBAL_TAG)
            tag = Log.GLOBAL_TAG;
        var str = "[".concat(tag, "] > ").concat(msg);
        if (Log.ENABLE_CALLBACK) {
            Log.emitter.emit('log', 'debug', str);
        }
        if (!Log.ENABLE_DEBUG) {
            return;
        }
        if (console.debug) {
            console.debug(str);
        }
        else {
            console.log(str);
        }
    };
    Log.v = function (tag, msg) {
        if (!tag || Log.FORCE_GLOBAL_TAG)
            tag = Log.GLOBAL_TAG;
        var str = "[".concat(tag, "] > ").concat(msg);
        if (Log.ENABLE_CALLBACK) {
            Log.emitter.emit('log', 'verbose', str);
        }
        if (!Log.ENABLE_VERBOSE) {
            return;
        }
        console.log(str);
    };
    return Log;
}());
Log.GLOBAL_TAG = 'mpegts.js';
Log.FORCE_GLOBAL_TAG = false;
Log.ENABLE_ERROR = true;
Log.ENABLE_INFO = true;
Log.ENABLE_WARN = true;
Log.ENABLE_DEBUG = true;
Log.ENABLE_VERBOSE = true;
Log.ENABLE_CALLBACK = false;
Log.emitter = new events__WEBPACK_IMPORTED_MODULE_0___default.a();
/* harmony default export */ __webpack_exports__["default"] = (Log);


/***/ }),

/***/ "./src/utils/logging-control.js":
/*!**************************************!*\
  !*** ./src/utils/logging-control.js ***!
  \**************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__(/*! events */ "./node_modules/events/events.js");
/* harmony import */ var events__WEBPACK_IMPORTED_MODULE_0___default = /*#__PURE__*/__webpack_require__.n(events__WEBPACK_IMPORTED_MODULE_0__);
/* harmony import */ var _logger_js__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__(/*! ./logger.js */ "./src/utils/logger.js");
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */


var LoggingControl = /** @class */ (function () {
    function LoggingControl() {
    }
    Object.defineProperty(LoggingControl, "forceGlobalTag", {
        get: function () {
            return _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].FORCE_GLOBAL_TAG;
        },
        set: function (enable) {
            _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].FORCE_GLOBAL_TAG = enable;
            LoggingControl._notifyChange();
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(LoggingControl, "globalTag", {
        get: function () {
            return _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].GLOBAL_TAG;
        },
        set: function (tag) {
            _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].GLOBAL_TAG = tag;
            LoggingControl._notifyChange();
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(LoggingControl, "enableAll", {
        get: function () {
            return _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_VERBOSE
                && _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_DEBUG
                && _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_INFO
                && _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_WARN
                && _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_ERROR;
        },
        set: function (enable) {
            _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_VERBOSE = enable;
            _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_DEBUG = enable;
            _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_INFO = enable;
            _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_WARN = enable;
            _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_ERROR = enable;
            LoggingControl._notifyChange();
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(LoggingControl, "enableDebug", {
        get: function () {
            return _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_DEBUG;
        },
        set: function (enable) {
            _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_DEBUG = enable;
            LoggingControl._notifyChange();
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(LoggingControl, "enableVerbose", {
        get: function () {
            return _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_VERBOSE;
        },
        set: function (enable) {
            _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_VERBOSE = enable;
            LoggingControl._notifyChange();
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(LoggingControl, "enableInfo", {
        get: function () {
            return _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_INFO;
        },
        set: function (enable) {
            _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_INFO = enable;
            LoggingControl._notifyChange();
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(LoggingControl, "enableWarn", {
        get: function () {
            return _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_WARN;
        },
        set: function (enable) {
            _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_WARN = enable;
            LoggingControl._notifyChange();
        },
        enumerable: false,
        configurable: true
    });
    Object.defineProperty(LoggingControl, "enableError", {
        get: function () {
            return _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_ERROR;
        },
        set: function (enable) {
            _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_ERROR = enable;
            LoggingControl._notifyChange();
        },
        enumerable: false,
        configurable: true
    });
    LoggingControl.getConfig = function () {
        return {
            globalTag: _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].GLOBAL_TAG,
            forceGlobalTag: _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].FORCE_GLOBAL_TAG,
            enableVerbose: _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_VERBOSE,
            enableDebug: _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_DEBUG,
            enableInfo: _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_INFO,
            enableWarn: _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_WARN,
            enableError: _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_ERROR,
            enableCallback: _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_CALLBACK
        };
    };
    LoggingControl.applyConfig = function (config) {
        _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].GLOBAL_TAG = config.globalTag;
        _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].FORCE_GLOBAL_TAG = config.forceGlobalTag;
        _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_VERBOSE = config.enableVerbose;
        _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_DEBUG = config.enableDebug;
        _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_INFO = config.enableInfo;
        _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_WARN = config.enableWarn;
        _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_ERROR = config.enableError;
        _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_CALLBACK = config.enableCallback;
    };
    LoggingControl._notifyChange = function () {
        var emitter = LoggingControl.emitter;
        if (emitter.listenerCount('change') > 0) {
            var config = LoggingControl.getConfig();
            emitter.emit('change', config);
        }
    };
    LoggingControl.registerListener = function (listener) {
        LoggingControl.emitter.addListener('change', listener);
    };
    LoggingControl.removeListener = function (listener) {
        LoggingControl.emitter.removeListener('change', listener);
    };
    LoggingControl.addLogListener = function (listener) {
        _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].emitter.addListener('log', listener);
        if (_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].emitter.listenerCount('log') > 0) {
            _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_CALLBACK = true;
            LoggingControl._notifyChange();
        }
    };
    LoggingControl.removeLogListener = function (listener) {
        _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].emitter.removeListener('log', listener);
        if (_logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].emitter.listenerCount('log') === 0) {
            _logger_js__WEBPACK_IMPORTED_MODULE_1__["default"].ENABLE_CALLBACK = false;
            LoggingControl._notifyChange();
        }
    };
    return LoggingControl;
}());
LoggingControl.emitter = new events__WEBPACK_IMPORTED_MODULE_0___default.a();
/* harmony default export */ __webpack_exports__["default"] = (LoggingControl);


/***/ }),

/***/ "./src/utils/polyfill.js":
/*!*******************************!*\
  !*** ./src/utils/polyfill.js ***!
  \*******************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
var Polyfill = /** @class */ (function () {
    function Polyfill() {
    }
    Polyfill.install = function () {
        // ES6 Object.setPrototypeOf
        Object.setPrototypeOf = Object.setPrototypeOf || function (obj, proto) {
            obj.__proto__ = proto;
            return obj;
        };
        // ES6 Object.assign
        Object.assign = Object.assign || function (target) {
            if (target === undefined || target === null) {
                throw new TypeError('Cannot convert undefined or null to object');
            }
            var output = Object(target);
            for (var i = 1; i < arguments.length; i++) {
                var source = arguments[i];
                if (source !== undefined && source !== null) {
                    for (var key in source) {
                        if (source.hasOwnProperty(key)) {
                            output[key] = source[key];
                        }
                    }
                }
            }
            return output;
        };
        // String.prototype.startsWith()
        if (!String.prototype.startsWith) {
            Object.defineProperty(String.prototype, 'startsWith', {
                value: function (search, rawPos) {
                    var pos = rawPos > 0 ? rawPos | 0 : 0;
                    return this.substring(pos, pos + search.length) === search;
                }
            });
        }
        // ES6 Promise (missing support in IE11)
        if (typeof self.Promise !== 'function') {
            __webpack_require__(/*! es6-promise */ "./node_modules/es6-promise/dist/es6-promise.js").polyfill();
        }
    };
    return Polyfill;
}());
Polyfill.install();
/* harmony default export */ __webpack_exports__["default"] = (Polyfill);


/***/ }),

/***/ "./src/utils/typedarray-equality.ts":
/*!******************************************!*\
  !*** ./src/utils/typedarray-equality.ts ***!
  \******************************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2022 magicxqq. All Rights Reserved.
 *
 * @author magicxqq <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
function isAligned16(a) {
    return a.byteOffset % 2 === 0 && a.byteLength % 2 === 0;
}
function isAligned32(a) {
    return a.byteOffset % 4 === 0 && a.byteLength % 4 === 0;
}
function compareArray(a, b) {
    for (var i = 0; i < a.length; i++) {
        if (a[i] !== b[i]) {
            return false;
        }
    }
    return true;
}
function equal8(a, b) {
    return compareArray(a, b);
}
function equal16(a, b) {
    var a16 = new Uint16Array(a.buffer, a.byteOffset, a.byteLength / 2);
    var b16 = new Uint16Array(b.buffer, b.byteOffset, b.byteLength / 2);
    return compareArray(a16, b16);
}
function equal32(a, b) {
    var a32 = new Uint32Array(a.buffer, a.byteOffset, a.byteLength / 4);
    var b32 = new Uint32Array(b.buffer, b.byteOffset, b.byteLength / 4);
    return compareArray(a32, b32);
}
function buffersAreEqual(a, b) {
    if (a.byteLength !== b.byteLength) {
        return false;
    }
    if (isAligned32(a) && isAligned32(b)) {
        return equal32(a, b);
    }
    if (isAligned16(a) && isAligned16(b)) {
        return equal16(a, b);
    }
    return equal8(a, b);
}
/* harmony default export */ __webpack_exports__["default"] = (buffersAreEqual);


/***/ }),

/***/ "./src/utils/utf8-conv.js":
/*!********************************!*\
  !*** ./src/utils/utf8-conv.js ***!
  \********************************/
/*! exports provided: default */
/***/ (function(module, __webpack_exports__, __webpack_require__) {

"use strict";
__webpack_require__.r(__webpack_exports__);
/*
 * Copyright (C) 2016 Bilibili. All Rights Reserved.
 *
 * This file is derived from C++ project libWinTF8 (https://github.com/m13253/libWinTF8)
 * @author zheng qian <xqq@xqq.im>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
function checkContinuation(uint8array, start, checkLength) {
    var array = uint8array;
    if (start + checkLength < array.length) {
        while (checkLength--) {
            if ((array[++start] & 0xC0) !== 0x80)
                return false;
        }
        return true;
    }
    else {
        return false;
    }
}
function decodeUTF8(uint8array) {
    var out = [];
    var input = uint8array;
    var i = 0;
    var length = uint8array.length;
    while (i < length) {
        if (input[i] < 0x80) {
            out.push(String.fromCharCode(input[i]));
            ++i;
            continue;
        }
        else if (input[i] < 0xC0) {
            // fallthrough
        }
        else if (input[i] < 0xE0) {
            if (checkContinuation(input, i, 1)) {
                var ucs4 = (input[i] & 0x1F) << 6 | (input[i + 1] & 0x3F);
                if (ucs4 >= 0x80) {
                    out.push(String.fromCharCode(ucs4 & 0xFFFF));
                    i += 2;
                    continue;
                }
            }
        }
        else if (input[i] < 0xF0) {
            if (checkContinuation(input, i, 2)) {
                var ucs4 = (input[i] & 0xF) << 12 | (input[i + 1] & 0x3F) << 6 | input[i + 2] & 0x3F;
                if (ucs4 >= 0x800 && (ucs4 & 0xF800) !== 0xD800) {
                    out.push(String.fromCharCode(ucs4 & 0xFFFF));
                    i += 3;
                    continue;
                }
            }
        }
        else if (input[i] < 0xF8) {
            if (checkContinuation(input, i, 3)) {
                var ucs4 = (input[i] & 0x7) << 18 | (input[i + 1] & 0x3F) << 12
                    | (input[i + 2] & 0x3F) << 6 | (input[i + 3] & 0x3F);
                if (ucs4 > 0x10000 && ucs4 < 0x110000) {
                    ucs4 -= 0x10000;
                    out.push(String.fromCharCode((ucs4 >>> 10) | 0xD800));
                    out.push(String.fromCharCode((ucs4 & 0x3FF) | 0xDC00));
                    i += 4;
                    continue;
                }
            }
        }
        out.push(String.fromCharCode(0xFFFD));
        ++i;
    }
    return out.join('');
}
/* harmony default export */ __webpack_exports__["default"] = (decodeUTF8);


/***/ }),

/***/ "./src/utils/webworkify-webpack.js":
/*!*****************************************!*\
  !*** ./src/utils/webworkify-webpack.js ***!
  \*****************************************/
/*! no static exports found */
/***/ (function(module, exports, __webpack_require__) {

function webpackBootstrapFunc(modules) {
    /******/ // The module cache
    /******/ var installedModules = {};
    /******/ // The require function
    /******/ function __webpack_require__(moduleId) {
        /******/ // Check if module is in cache
        /******/ if (installedModules[moduleId])
            /******/ return installedModules[moduleId].exports;
        /******/ // Create a new module (and put it into the cache)
        /******/ var module = installedModules[moduleId] = {
            /******/ i: moduleId,
            /******/ l: false,
            /******/ exports: {}
            /******/ 
        };
        /******/ // Execute the module function
        /******/ modules[moduleId].call(module.exports, module, module.exports, __webpack_require__);
        /******/ // Flag the module as loaded
        /******/ module.l = true;
        /******/ // Return the exports of the module
        /******/ return module.exports;
        /******/ 
    }
    /******/ // expose the modules object (__webpack_modules__)
    /******/ __webpack_require__.m = modules;
    /******/ // expose the module cache
    /******/ __webpack_require__.c = installedModules;
    /******/ // identity function for calling harmony imports with the correct context
    /******/ __webpack_require__.i = function (value) { return value; };
    /******/ // define getter function for harmony exports
    /******/ __webpack_require__.d = function (exports, name, getter) {
        /******/ if (!__webpack_require__.o(exports, name)) {
            /******/ Object.defineProperty(exports, name, {
                /******/ configurable: false,
                /******/ enumerable: true,
                /******/ get: getter
                /******/ 
            });
            /******/ }
        /******/ 
    };
    /******/ // define __esModule on exports
    /******/ __webpack_require__.r = function (exports) {
        /******/ Object.defineProperty(exports, '__esModule', { value: true });
        /******/ 
    };
    /******/ // getDefaultExport function for compatibility with non-harmony modules
    /******/ __webpack_require__.n = function (module) {
        /******/ var getter = module && module.__esModule ?
            /******/ function getDefault() { return module['default']; } :
            /******/ function getModuleExports() { return module; };
        /******/ __webpack_require__.d(getter, 'a', getter);
        /******/ return getter;
        /******/ 
    };
    /******/ // Object.prototype.hasOwnProperty.call
    /******/ __webpack_require__.o = function (object, property) { return Object.prototype.hasOwnProperty.call(object, property); };
    /******/ // __webpack_public_path__
    /******/ __webpack_require__.p = "/";
    /******/ // on error function for async loading
    /******/ __webpack_require__.oe = function (err) { console.error(err); throw err; };
    var f = __webpack_require__(__webpack_require__.s = ENTRY_MODULE);
    return f.default || f; // try to call default if defined to also support babel esmodule exports
}
var moduleNameReqExp = '[\\.|\\-|\\+|\\w|\/|@]+';
var dependencyRegExp = '\\(\\s*(\/\\*.*?\\*\/)?\\s*.*?(' + moduleNameReqExp + ').*?\\)'; // additional chars when output.pathinfo is true
// http://stackoverflow.com/a/2593661/130442
function quoteRegExp(str) {
    return (str + '').replace(/[.?*+^$[\]\\(){}|-]/g, '\\$&');
}
function isNumeric(n) {
    return !isNaN(1 * n); // 1 * n converts integers, integers as string ("123"), 1e3 and "1e3" to integers and strings to NaN
}
function getModuleDependencies(sources, module, queueName) {
    var retval = {};
    retval[queueName] = [];
    var fnString = module.toString();
    var wrapperSignature = fnString.match(/^function\s?\w*\(\w+,\s*\w+,\s*(\w+)\)/);
    if (!wrapperSignature)
        return retval;
    var webpackRequireName = wrapperSignature[1];
    // main bundle deps
    var re = new RegExp('(\\\\n|\\W)' + quoteRegExp(webpackRequireName) + dependencyRegExp, 'g');
    var match;
    while ((match = re.exec(fnString))) {
        if (match[3] === 'dll-reference')
            continue;
        retval[queueName].push(match[3]);
    }
    // dll deps
    re = new RegExp('\\(' + quoteRegExp(webpackRequireName) + '\\("(dll-reference\\s(' + moduleNameReqExp + '))"\\)\\)' + dependencyRegExp, 'g');
    while ((match = re.exec(fnString))) {
        if (!sources[match[2]]) {
            retval[queueName].push(match[1]);
            sources[match[2]] = __webpack_require__(match[1]).m;
        }
        retval[match[2]] = retval[match[2]] || [];
        retval[match[2]].push(match[4]);
    }
    // convert 1e3 back to 1000 - this can be important after uglify-js converted 1000 to 1e3
    var keys = Object.keys(retval);
    for (var i = 0; i < keys.length; i++) {
        for (var j = 0; j < retval[keys[i]].length; j++) {
            if (isNumeric(retval[keys[i]][j])) {
                retval[keys[i]][j] = 1 * retval[keys[i]][j];
            }
        }
    }
    return retval;
}
function hasValuesInQueues(queues) {
    var keys = Object.keys(queues);
    return keys.reduce(function (hasValues, key) {
        return hasValues || queues[key].length > 0;
    }, false);
}
function getRequiredModules(sources, moduleId) {
    var modulesQueue = {
        main: [moduleId]
    };
    var requiredModules = {
        main: []
    };
    var seenModules = {
        main: {}
    };
    while (hasValuesInQueues(modulesQueue)) {
        var queues = Object.keys(modulesQueue);
        for (var i = 0; i < queues.length; i++) {
            var queueName = queues[i];
            var queue = modulesQueue[queueName];
            var moduleToCheck = queue.pop();
            seenModules[queueName] = seenModules[queueName] || {};
            if (seenModules[queueName][moduleToCheck] || !sources[queueName][moduleToCheck])
                continue;
            seenModules[queueName][moduleToCheck] = true;
            requiredModules[queueName] = requiredModules[queueName] || [];
            requiredModules[queueName].push(moduleToCheck);
            var newModules = getModuleDependencies(sources, sources[queueName][moduleToCheck], queueName);
            var newModulesKeys = Object.keys(newModules);
            for (var j = 0; j < newModulesKeys.length; j++) {
                modulesQueue[newModulesKeys[j]] = modulesQueue[newModulesKeys[j]] || [];
                modulesQueue[newModulesKeys[j]] = modulesQueue[newModulesKeys[j]].concat(newModules[newModulesKeys[j]]);
            }
        }
    }
    return requiredModules;
}
module.exports = function (moduleId, options) {
    options = options || {};
    var sources = {
        main: __webpack_require__.m
    };
    var requiredModules = options.all ? { main: Object.keys(sources.main) } : getRequiredModules(sources, moduleId);
    var src = '';
    Object.keys(requiredModules).filter(function (m) { return m !== 'main'; }).forEach(function (module) {
        var entryModule = 0;
        while (requiredModules[module][entryModule]) {
            entryModule++;
        }
        requiredModules[module].push(entryModule);
        sources[module][entryModule] = '(function(module, exports, __webpack_require__) { module.exports = __webpack_require__; })';
        src = src + 'var ' + module + ' = (' + webpackBootstrapFunc.toString().replace('ENTRY_MODULE', JSON.stringify(entryModule)) + ')({' + requiredModules[module].map(function (id) { return '' + JSON.stringify(id) + ': ' + sources[module][id].toString(); }).join(',') + '});\n';
    });
    src = src + 'new ((' + webpackBootstrapFunc.toString().replace('ENTRY_MODULE', JSON.stringify(moduleId)) + ')({' + requiredModules.main.map(function (id) { return '' + JSON.stringify(id) + ': ' + sources.main[id].toString(); }).join(',') + '}))(self);';
    var blob = new self.Blob([src], { type: 'text/javascript' });
    if (options.bare) {
        return blob;
    }
    var URL = self.URL || self.webkitURL || self.mozURL || self.msURL;
    var workerUrl = URL.createObjectURL(blob);
    var worker = new self.Worker(workerUrl);
    worker.objectURL = workerUrl;
    return worker;
};


/***/ })

/******/ });
});
//# sourceMappingURL=mpegts.js.map