// Unit tests for create_bot payload building (no network).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildCreateBotPayload as build } from '../src/api.js';

test('teams_login_domain builds a signed-in teams block', () => {
  const p = build({ meetingLink: 'https://teams.microsoft.com/l/x', teamsLoginDomain: 'bots.acme.com', signInEmail: 'bot1@bots.acme.com', strictEmail: false });
  assert.deepEqual(p.teams, { login_required: true, teams_login_domain: 'bots.acme.com', sign_in_email: 'bot1@bots.acme.com', strict_email: false });
  assert.equal(p.google_meet, undefined);
});

test('google_login_domain builds a signed-in google_meet block', () => {
  const p = build({ meetingLink: 'https://meet.google.com/x', googleLoginDomain: 'acme.com' });
  assert.deepEqual(p.google_meet, { login_required: true, google_login_domain: 'acme.com' });
});

test('signed-in and zoom options reject invalid combinations', () => {
  assert.throws(() => build({ meetingLink: 'm', googleLoginDomain: 'a', teamsLoginDomain: 'b' }), /only one/);
  assert.throws(() => build({ meetingLink: 'm', signInEmail: 'x@y' }), /need google_login_domain or teams_login_domain/);
  assert.throws(() => build({ meetingLink: 'm', zoomZakUrl: 'https://a', zoomObfUrl: 'https://b' }), /only one/);
});

test('zoom token URLs map to zoom.zak_url / zoom.obf_url', () => {
  assert.deepEqual(build({ meetingLink: 'https://zoom.us/j/1', zoomZakUrl: 'https://x/zak' }).zoom, { zak_url: 'https://x/zak' });
  assert.deepEqual(build({ meetingLink: 'https://zoom.us/j/1', zoomObfUrl: 'https://x/obf' }).zoom, { obf_url: 'https://x/obf' });
  assert.equal(build({ meetingLink: 'https://zoom.us/j/1' }).zoom, undefined);
});

test('video is off by default and speaker view is explicit when it is on', () => {
  const audio = build({ meetingLink: 'https://meet.google.com/x' });
  assert.equal(audio.video_required, false);
  assert.equal(audio.recording_config?.video_layout, undefined); // layout is meaningless without video

  const video = build({ meetingLink: 'https://meet.google.com/x', video: true });
  assert.equal(video.video_required, true);
  assert.equal(video.recording_config.video_layout, 'speaker_view'); // API default is grid_view, so send it
  assert.equal(video.video_separate_streams, undefined);            // per-participant video stays opt-in

  assert.equal(build({ meetingLink: 'm', video: true, videoLayout: 'grid_view' }).recording_config.video_layout, 'grid_view');
});

test('video_layout is validated and requires video', () => {
  assert.throws(() => build({ meetingLink: 'm', videoLayout: 'speaker_view' }), /needs record_video/);
  assert.throws(() => build({ meetingLink: 'm', video: true, videoLayout: 'gallery' }), /speaker_view.*grid_view/);
});
