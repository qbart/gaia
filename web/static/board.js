(function ($) {
  'use strict';

  $(function () {
    var $board = $('.gaia-board-columns');
    initBoardPanScroll($board);
    initSortable($board);
  });

  // Click-and-drag horizontal scrolling on the board background. Skips
  // cards (which have HTML5 drag handlers) and other interactive controls.
  function initBoardPanScroll($board) {
    if (!$board.length) return;

    var dragging = false;
    var startX = 0;
    var startScroll = 0;

    $board.on('mousedown', function (e) {
      if (e.which !== 1) return;
      var $target = $(e.target);
      if ($target.closest('.gaia-card, .gaia-add-card, a, button, input, textarea, [data-sortable="true"]').length) {
        return;
      }
      dragging = true;
      startX = e.pageX;
      startScroll = $board.scrollLeft();
      $board.addClass('gaia-board-grabbing');
      e.preventDefault();
    });

    $(document).on('mousemove.gaiaBoardPan', function (e) {
      if (!dragging) return;
      $board.scrollLeft(startScroll - (e.pageX - startX));
    });

    $(document).on('mouseup.gaiaBoardPan mouseleave.gaiaBoardPan', function () {
      if (!dragging) return;
      dragging = false;
      $board.removeClass('gaia-board-grabbing');
    });
  }

  // Card sortable across all sortable columns. Cross-column drops POST a
  // status change first, then save the destination order. Single-column
  // drops just save the order. The doing column has no data-sortable so
  // the browser refuses to accept drops there.
  function initSortable($board) {
    if (!$board.length) return;

    var $dragSrc = null;
    var dropped = false;

    $board.on('dragstart', '.gaia-card[draggable="true"]', function (e) {
      var $card = $(this);
      $card.addClass('gaia-card-dragging');
      $dragSrc = $card.closest('[data-sortable="true"]');
      var dt = e.originalEvent.dataTransfer;
      dt.effectAllowed = 'move';
      try { dt.setData('text/plain', $card.data('task-id') || ''); } catch (_) {}
    });

    $board.on('dragover', '[data-sortable="true"]', function (e) {
      e.preventDefault();
      e.originalEvent.dataTransfer.dropEffect = 'move';
      var $col = $(this);
      var $dragging = $('.gaia-card-dragging');
      if (!$dragging.length) return;
      var $closest = closestCardAfter($col, e.originalEvent.clientY, $dragging[0]);
      if (!$closest) {
        $col.append($dragging);
      } else {
        $closest.before($dragging);
      }
    });

    $board.on('dragend', '.gaia-card', function () {
      var $card = $(this);
      $card.removeClass('gaia-card-dragging');
      dropped = true;

      var $dest = $card.closest('[data-sortable="true"]');
      var src = $dragSrc;
      $dragSrc = null;
      if (!$dest.length) return;

      var taskID = $card.data('task-id');
      var projectID = $dest.data('project-id');
      var destStatus = $dest.data('status');
      var movedColumns = !src || !src.length || src[0] !== $dest[0];

      if (movedColumns) {
        $.ajax({
          url: '/projects/' + encodeURIComponent(projectID) +
               '/tasks/' + encodeURIComponent(taskID) + '/move',
          type: 'POST',
          data: { status: destStatus }
        }).done(function () {
          saveOrder($dest, projectID, destStatus);
        }).fail(function () {
          window.location.reload();
        });
      } else {
        saveOrder($dest, projectID, destStatus);
      }
    });

    // Suppress the click that follows a drop on an <a> card so the user
    // doesn't get yanked onto the edit page after rearranging.
    $board.on('click', '.gaia-card', function (e) {
      if (dropped) {
        dropped = false;
        e.preventDefault();
      }
    });
  }

  function closestCardAfter($column, y, draggingEl) {
    var closest = null;
    var closestOffset = Number.NEGATIVE_INFINITY;
    $column.children('.gaia-card').each(function () {
      if (this === draggingEl) return;
      var box = this.getBoundingClientRect();
      var offset = y - box.top - box.height / 2;
      if (offset < 0 && offset > closestOffset) {
        closestOffset = offset;
        closest = $(this);
      }
    });
    return closest;
  }

  function saveOrder($column, projectID, status) {
    var ids = $column.children('.gaia-card').map(function () {
      return $(this).data('task-id');
    }).get().filter(function (id) { return id !== undefined && id !== ''; });

    $.ajax({
      url: '/projects/' + encodeURIComponent(projectID) +
           '/columns/' + encodeURIComponent(status) + '/order',
      type: 'POST',
      data: { ids: ids.join(',') }
    }).fail(function () {
      window.location.reload();
    });
  }
})(jQuery);
