(function () {
    function init() {
        var board = document.querySelector('.gaia-board-columns');
        if (!board) return;

        var isDown = false;
        var startX = 0;
        var startScroll = 0;
        var moved = false;

        board.addEventListener('mousedown', function (e) {
            if (e.button !== 0) return;
            if (e.target !== board) return;
            isDown = true;
            moved = false;
            startX = e.pageX;
            startScroll = board.scrollLeft;
            board.classList.add('gaia-board-grabbing');
            e.preventDefault();
        });

        window.addEventListener('mouseup', function () {
            if (!isDown) return;
            isDown = false;
            board.classList.remove('gaia-board-grabbing');
        });

        window.addEventListener('mousemove', function (e) {
            if (!isDown) return;
            var dx = e.pageX - startX;
            if (Math.abs(dx) > 3) moved = true;
            board.scrollLeft = startScroll - dx;
        });

        board.addEventListener('click', function (e) {
            if (moved) {
                e.preventDefault();
                e.stopPropagation();
                moved = false;
            }
        }, true);

        board.addEventListener('wheel', function (e) {
            if (e.deltaY === 0 || e.shiftKey) return;
            if (e.target !== board) return;
            board.scrollLeft += e.deltaY;
            e.preventDefault();
        }, { passive: false });
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
