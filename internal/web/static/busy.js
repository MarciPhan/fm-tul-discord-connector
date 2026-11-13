// Okamzita zpetna vazba po kliknuti na tlacitka overeni (presmerovani muze chvili trvat)
(function () {
	function busy(el) {
		el.classList.add('busy');
		el.setAttribute('aria-busy', 'true');
	}
	document.addEventListener('click', function (e) {
		var a = e.target.closest && e.target.closest('a.btn');
		if (!a || e.defaultPrevented || e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
		busy(a);
	});
	document.addEventListener('submit', function (e) {
		if (e.submitter) busy(e.submitter);
	});
	window.addEventListener('pageshow', function () {
		document.querySelectorAll('.btn.busy').forEach(function (b) {
			b.classList.remove('busy');
			b.removeAttribute('aria-busy');
		});
	});
})();
