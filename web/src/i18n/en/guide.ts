import type { guide as zh } from '../zh-CN/guide'

export const guide = {
  title: 'Guide',
  description:
    'Plug the image host into your blog, notes and screenshot workflow. Addresses on this page are generated for this site and can be copied as they are.',
  sections: {
    quickStart: 'Quick start',
    curl: 'curl / scripts',
    picgo: 'PicGo / PicList',
    s3: 'S3 clients',
    urlFetch: 'Fetch from a URL',
    access: 'Links and resizing',
    errors: 'Upload error codes',
    webUpload: 'Upload from the admin UI',
  },
  step1Title: 'Check storage channels',
  step1Desc:
    'The built-in local channel works out of the box; to add S3 / R2 and the like, create a channel on the storage channels page (admin only).',
  step2Title: 'Issue an upload token',
  step2Desc:
    'On the upload tokens page, issue one named token per client (PicGo, scripts). The secret is shown only once; uploading from the admin UI needs no token.',
  step3Title: 'Start uploading',
  step3Desc:
    'Call the upload API from curl / PicGo with the token, or upload straight from the images page.',
  goTo: {
    channels: 'Storage channels',
    tokens: 'Upload tokens',
    images: 'Images',
    settings: 'Settings',
  },
  curlIntro:
    'Anything that can send an HTTP request can upload; tokens are issued on the upload tokens page.',
  fields: {
    file: 'Required multipart file field; format is detected from content, only jpeg / png / gif / webp are accepted',
    ttl: 'Optional lifetime in seconds; 0 or never means permanent, leave empty to follow the default (user default, otherwise the global default)',
    exifPolicy:
      'Optional: strip (remove metadata) / passthrough (keep as is); leave empty to follow the global default',
    idempotencyKey:
      'Optional request header; retrying with the same key never creates a second image',
  },
  response: 'Success response (200)',
  picgoIntro:
    'Use the custom uploader built into PicList or the web-uploader plugin for PicGo and fill in the fields below:',
  picgo: {
    apiUrl: 'API URL',
    method: 'Method',
    fileField: 'POST field name',
    headers: 'Custom headers',
    jsonPath: 'URL path in the response (JSON path)',
  },
  baseUrlNote:
    'Without a public site address configured, the returned url is derived from the request host, which works out of the box for direct access. Behind a reverse proxy / CDN whose public domain differs from the request host, set base_url to your public domain under Settings → Runtime config.',
  s3Intro:
    'PicGo / PicList / Typora / iOS Shortcuts all ship an "Amazon S3" uploader, and this site also accepts images over the S3 protocol, so pick it, no plugin needed. On the upload tokens page, open "S3 config" on a token and copy the values into the client.',
  s3: {
    credentials: 'Credentials',
    credentialsDesc:
      'The access key is the token prefix and the secret is derived from the master key; both can be viewed again in the admin UI. Revoking the token closes the S3 path as well',
    bucket: 'Bucket / region',
    bucketDesc:
      'This site has no buckets or regions; use the defaults shown in the dialog',
    style: 'Addressing style',
    styleDesc: 'Path style; enable that switch if the client has one',
    urls: 'Image URLs',
    urlsDesc:
      'Built from the "custom domain" shown in the dialog, in the form /o/<user id>/<object key>',
    ops: 'Supported operations',
    opsDesc:
      'PutObject, HeadBucket, HeadObject, GetObject, DeleteObject; no bucket listing, multipart upload or ACLs',
  },
  s3Note:
    'Uploading the same object key again overwrites in S3 terms: the key points at the new image while the old short link keeps working.',
  urlFetchIntro:
    'Give an image URL and the server fetches it into this site, handy for pulling images scattered on other hosts back under your control. The upload dialog on the images page offers the same thing.',
  urlFetchFields: {
    url: 'Required image URL, http / https only',
    fileName: 'Optional, overrides the file name inferred from the URL',
    ttlAndExif: 'Optional ttl and exif_policy, same as the form upload',
  },
  urlFetchWarnTitle: 'Public http / https addresses only',
  urlFetchWarnDesc:
    'Requests to private, loopback or cloud metadata addresses are rejected, including every hop of a redirect, so nobody can use this service to probe your internal network.',
  accessIntro:
    'Every image has a short link; images uploaded through an S3 client also get an alias by object key. Both point at the same image.',
  access: {
    shortLink: 'Short link',
    shortLinkDesc:
      'The url in the upload response; stops working the moment the image is deleted',
    alias: 'Object key alias',
    aliasDesc:
      '/o/<user id>/<object key>; Markdown links pasted by S3 clients work as they are',
    width: 'Resizing',
    widthDesc:
      'Append ?w=400 to resize to a width tier; tiers come from process_widths under Settings → Runtime config, and widths outside the tiers return 400',
    format: 'Format',
    formatDesc:
      'Without parameters the browser Accept header picks WebP automatically; ?format=webp / jpeg / png forces one',
  },
  errorCols: {
    code: 'Status',
    meaning: 'Meaning',
    advice: 'What to do',
  },
  errorRows: {
    400: {
      meaning:
        'Bad request: file field missing, image content unreadable, invalid ttl / exif_policy',
      advice: 'Check the form fields and value formats',
    },
    401: {
      meaning: 'Token invalid, revoked or expired',
      advice: 'Issue or rotate the token on the upload tokens page',
    },
    403: {
      meaning: 'Token lacks upload permission, or the storage quota is full',
      advice:
        'Check the token permissions; when the quota is full, delete images or ask an admin to raise it',
    },
    413: {
      meaning: 'File exceeds the per-image size limit',
      advice: 'Compress and retry, or raise max_upload_mb',
    },
    415: {
      meaning: 'Unsupported file type',
      advice:
        'Only jpeg / png / gif / webp, detected from content rather than the extension',
    },
    422: {
      meaning: 'Image resolution exceeds the pixel budget',
      advice: 'Downscale and retry',
    },
    429: {
      meaning: 'Too many uploads or all upload slots busy',
      advice: 'Retry after the Retry-After header',
    },
    503: {
      meaning: 'Storage channel temporarily unavailable',
      advice:
        'Retry later; if it persists, check channel health on the channels page',
    },
  },
  webUploadIntro:
    'Prefer no client setup? Click "Upload" on the images page; it uses your signed-in identity and needs no token. Every uploaded image gets a short link you can copy in one click.',
} satisfies typeof zh
