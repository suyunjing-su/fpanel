/// <reference types="vite/client" />

interface GeeTestInstance {
	appendTo: (selector: string) => GeeTestInstance;
	onSuccess: (callback: () => void) => GeeTestInstance;
	onError?: (callback: () => void) => GeeTestInstance;
	getValidate: () => Record<string, string>;
	showBox: () => void;
	destroy?: () => void;
}

interface GeeTestOptions {
	captchaId: string;
	product?: 'bind' | 'float' | 'popup';
}

interface Window {
	initGeetest4?: (options: GeeTestOptions, callback: (instance: GeeTestInstance) => void) => void;
}
