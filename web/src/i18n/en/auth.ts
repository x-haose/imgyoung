import type { auth as zh } from '../zh-CN/auth'

export const auth = {
  login: 'Sign in',
  loginTitle: 'Sign in to ImgYoung',
  username: 'Username',
  password: 'Password',
  usernameRequired: 'Enter your username',
  passwordRequired: 'Enter your password',
  loginHint:
    'First sign-in: username admin, default password {password}. You will be asked to change it right away.',
  forgotHint: 'Forgot the password? See the reset steps in the README.',
  account: 'Account',
  role: { admin: 'Administrator', user: 'User' },
  signOut: 'Sign out',
  signOutConfirm: 'Sign out of ImgYoung?',
  changePassword: 'Change password',
  oldPassword: 'Current password',
  newPassword: 'New password',
  confirmPassword: 'Confirm new password',
  oldPasswordRequired: 'Enter your current password',
  newPasswordRequired: 'Enter a new password',
  confirmRequired: 'Enter the new password again',
  passwordTooShort: 'At least {min} characters',
  passwordMismatch: 'Passwords do not match',
  changePasswordNote:
    'All signed-in devices will be signed out and must sign in again.',
  passwordChanged: 'Password changed, please sign in again',
  firstRunTitle: 'Set the admin password',
  firstRunSubtitle: 'First sign-in · account {name}',
  firstRunWarningTitle: 'The factory default password is in use',
  firstRunWarningDesc:
    'The default password is public; anyone can sign in with it. Change it now to one only you know.',
  firstRunSubmit: 'Save and sign in again',
  firstRunDone: 'Password set, please sign in with the new password',
} satisfies typeof zh
