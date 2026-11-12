// Zmensi zakladni velikost pisma, dokud se cela stranka nevejde na jednu obrazovku bez rolovani.
(function () {
	var root = document.documentElement;
	function fit() {
		var size = 16;
		root.style.fontSize = size + "px";
		root.classList.remove("fit");
		while (document.body.offsetHeight > window.innerHeight + 1 && size > 9) {
			size -= 0.5;
			root.style.fontSize = size + "px";
		}
		if (document.body.offsetHeight <= window.innerHeight + 1) {
			root.classList.add("fit");
		}
	}
	var timer;
	window.addEventListener("resize", function () {
		clearTimeout(timer);
		timer = setTimeout(fit, 80);
	});
	if (document.fonts && document.fonts.ready) {
		document.fonts.ready.then(fit);
	}
	window.addEventListener("load", fit);
	fit();
})();
