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

interface TurnstileOptions {
	sitekey: string;
	theme?: 'light' | 'dark' | 'auto';
	callback: (token: string) => void;
	'expired-callback'?: () => void;
	'error-callback'?: () => boolean;
}

interface TurnstileAPI {
	render: (container: HTMLElement, options: TurnstileOptions) => string;
	reset: (widgetId: string) => void;
	remove: (widgetId: string) => void;
}

interface Window {
	initGeetest4?: (options: GeeTestOptions, callback: (instance: GeeTestInstance) => void) => void;
	turnstile?: TurnstileAPI;
}
